package looprunner

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/heruujoko/piramid/internal/cron"
	"github.com/heruujoko/piramid/internal/definitions"
	"github.com/heruujoko/piramid/internal/domain"
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

type IDGenerator func(prefix string, now time.Time, loopID string) string

type Config struct {
	Source       DefinitionsSource
	Store        SchedulerStore
	Clock        Clock
	PollInterval time.Duration
	IDGenerator  IDGenerator
}

type Scheduler struct {
	source       DefinitionsSource
	store        SchedulerStore
	clock        Clock
	pollInterval time.Duration
	idGenerator  IDGenerator
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
	pollInterval := config.PollInterval
	if pollInterval <= 0 {
		pollInterval = time.Minute
	}
	return &Scheduler{
		source:       config.Source,
		store:        config.Store,
		clock:        clock,
		pollInterval: pollInterval,
		idGenerator:  idGenerator,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	for {
		_ = s.Tick(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.pollInterval):
		}
	}
}

func (s *Scheduler) Tick(ctx context.Context) error {
	snapshot, err := s.source.Load(ctx)
	if err != nil {
		return err
	}
	now := s.clock.Now().UTC().Truncate(time.Minute)
	for _, loop := range snapshot.Loops {
		if !loop.Active {
			continue
		}
		due, err := s.loopDue(ctx, loop, now)
		if err != nil {
			return err
		}
		if !due {
			continue
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
		if _, err := s.store.CreateFire(ctx, fire); err != nil {
			return err
		}
		if err := s.store.SaveGoalDraft(ctx, goal, storepkg.PersistedPaths{}); err != nil {
			return err
		}
	}
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

func defaultIDGenerator(prefix string, now time.Time, loopID string) string {
	safeLoopID := strings.NewReplacer("/", "-", " ", "-", "_", "-").Replace(loopID)
	return fmt.Sprintf("%s-%s-%s", prefix, safeLoopID, now.Format("20060102150405"))
}
