package looprunner

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/heruujoko/piramid/internal/cron"
	"github.com/heruujoko/piramid/internal/definitions"
	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/records"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

type DefinitionsSource interface {
	Load(context.Context) (definitions.Snapshot, error)
}

type Clock interface {
	Now() time.Time
}

type SchedulerStore interface {
	GetLatestFireByLoop(context.Context, string) (domain.Fire, error)
	CreateFire(context.Context, domain.Fire) (domain.Fire, error)
	SaveGoalDraft(context.Context, domain.Goal, storepkg.PersistedPaths) error
}

type DraftFireStore interface {
	CreateDraftFire(context.Context, domain.Goal, storepkg.PersistedPaths, domain.Fire) error
}

type IDGenerator func(prefix string, now time.Time, loopID string) string

type PlanGenerator func(domain.Loop, string, time.Time) domain.Plan

type ErrorHandler func(error)

type Logger interface {
	Printf(string, ...any)
}

type Config struct {
	Source        DefinitionsSource
	Store         SchedulerStore
	Clock         Clock
	PollInterval  time.Duration
	IDGenerator   IDGenerator
	PlanGenerator PlanGenerator
	Records       *records.Store
	OnError       ErrorHandler
	Logger        Logger
}

type Scheduler struct {
	source        DefinitionsSource
	store         SchedulerStore
	clock         Clock
	pollInterval  time.Duration
	idGenerator   IDGenerator
	planGenerator PlanGenerator
	records       *records.Store
	onError       ErrorHandler
	logger        Logger
}

func NewScheduler(config Config) *Scheduler {
	clock := config.Clock
	if clock == nil {
		clock = systemClock{}
	}
	idGenerator := config.IDGenerator
	if idGenerator == nil {
		idGenerator = defaultIDGenerator
	}
	planGenerator := config.PlanGenerator
	if planGenerator == nil {
		planGenerator = defaultPlanGenerator
	}
	pollInterval := config.PollInterval
	if pollInterval <= 0 {
		pollInterval = time.Minute
	}
	return &Scheduler{
		source:        config.Source,
		store:         config.Store,
		clock:         clock,
		pollInterval:  pollInterval,
		idGenerator:   idGenerator,
		planGenerator: planGenerator,
		records:       config.Records,
		onError:       config.OnError,
		logger:        config.Logger,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	for {
		if err := s.Tick(ctx); err != nil && s.onError != nil {
			s.onError(err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.pollInterval):
		}
	}
}

func (s *Scheduler) Tick(ctx context.Context) error {
	s.logf("loop scheduler tick started")
	snapshot, err := s.source.Load(ctx)
	if err != nil {
		s.logf("failed to load loop definitions")
		return err
	}
	now := s.clock.Now().UTC().Truncate(time.Minute)
	var tickErr error
	for _, loop := range snapshot.Loops {
		if !loop.Active {
			continue
		}
		if err := s.tickLoop(ctx, loop, now); err != nil {
			s.logf("loop tick failed")
			tickErr = errors.Join(tickErr, err)
		}
	}
	if tickErr == nil {
		s.logf("loop scheduler tick completed")
	}
	return tickErr
}

func (s *Scheduler) tickLoop(ctx context.Context, loop domain.Loop, now time.Time) error {
	due, err := s.loopDue(ctx, loop, now)
	if err != nil {
		return err
	}
	if !due {
		return nil
	}
	goalID := s.idGenerator("GOAL", now, loop.ID)
	fireID := s.idGenerator("FIRE", now, loop.ID)
	fire := domain.Fire{
		ID:          fireID,
		LoopID:      loop.ID,
		GoalID:      goalID,
		Status:      domain.FireScheduled,
		ScheduledAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	goal := domain.Goal{
		ID:          goalID,
		FireID:      fireID,
		Text:        loop.Goal,
		ProjectPath: loop.ProjectPath,
		Status:      domain.GoalDraft,
		CreatedAt:   now,
	}
	paths := storepkg.PersistedPaths{}
	if s.records != nil {
		goalRecord, err := s.records.WriteGoal(goal)
		if err != nil {
			return err
		}
		planRecord, err := s.records.WritePlan(goalID, s.planGenerator(loop, goalID, now))
		if err != nil {
			return err
		}
		paths.GoalPath = goalRecord.Path
		paths.PlanPath = planRecord.Path
	}
	if draftFireStore, ok := s.store.(DraftFireStore); ok {
		if err := draftFireStore.CreateDraftFire(ctx, goal, paths, fire); err != nil {
			return err
		}
		s.logf("created fire")
		return nil
	}
	if err := s.store.SaveGoalDraft(ctx, goal, paths); err != nil {
		return err
	}
	if _, err := s.store.CreateFire(ctx, fire); err != nil {
		return err
	}
	s.logf("created fire")
	return nil
}

func (s *Scheduler) loopDue(ctx context.Context, loop domain.Loop, now time.Time) (bool, error) {
	latest, err := s.store.GetLatestFireByLoop(ctx, loop.ID)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	anchor := now.Add(-time.Minute)
	if err == nil {
		anchor = latest.ScheduledAt
	}
	next, err := cron.NextAfter(loop.Cron, anchor)
	if err != nil {
		return false, err
	}
	return !next.After(now), nil
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func (s *Scheduler) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}

func defaultIDGenerator(prefix string, now time.Time, loopID string) string {
	safeLoopID := safeIdentifier(loopID)
	return fmt.Sprintf("%s-%s-%s", prefix, safeLoopID, now.Format("20060102150405"))
}

func defaultPlanGenerator(loop domain.Loop, goalID string, now time.Time) domain.Plan {
	taskID := defaultIDGenerator("TASK", now, loop.ID)
	return domain.Plan{
		Version: 1,
		GoalID:  goalID,
		Tasks: []domain.Task{{
			ID:          taskID,
			Title:       loop.Goal,
			Goal:        loop.Goal,
			ProjectPath: loop.ProjectPath,
			DOD:         []string{"Loop goal is complete."},
			MaxAttempts: 1,
			Timeout:     time.Hour,
			TimeoutText: time.Hour.String(),
		}},
	}
}

func safeIdentifier(value string) string {
	return hex.EncodeToString([]byte(value))
}
