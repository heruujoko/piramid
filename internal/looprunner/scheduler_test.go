package looprunner

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/definitions"
	"github.com/heruujoko/piramid/internal/domain"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

func TestTickCreatesFireAndDraftGoalWhenLoopIsDue(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	store := &fakeStore{latestErr: sql.ErrNoRows}
	scheduler := NewScheduler(Config{
		Source: staticSource{snapshot: definitions.Snapshot{Loops: []domain.Loop{testLoop("loop-a", true, "0 9 * * *")}}},
		Store:  store,
		Clock:  fixedClock{now: now},
	})

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(store.fires) != 1 {
		t.Fatalf("fires = %#v, want one", store.fires)
	}
	if store.fires[0].LoopID != "loop-a" {
		t.Fatalf("fire loop = %q", store.fires[0].LoopID)
	}
	if store.fires[0].Status != domain.FireScheduled {
		t.Fatalf("fire status = %s", store.fires[0].Status)
	}
	if !store.fires[0].ScheduledAt.Equal(now) {
		t.Fatalf("scheduled_at = %v, want %v", store.fires[0].ScheduledAt, now)
	}
	if len(store.goals) != 1 {
		t.Fatalf("goals = %#v, want one", store.goals)
	}
	if store.goals[0].Status != domain.GoalDraft {
		t.Fatalf("goal status = %s, want DRAFT", store.goals[0].Status)
	}
	if store.goals[0].Text != "Maintain project" || store.goals[0].ProjectPath != "/tmp/project" {
		t.Fatalf("goal = %#v", store.goals[0])
	}
	if store.fires[0].GoalID == "" || store.goals[0].ID != store.fires[0].GoalID {
		t.Fatalf("fire and goal not linked: fire=%#v goal=%#v", store.fires[0], store.goals[0])
	}
}

func TestTickSkipsInactiveLoops(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	store := &fakeStore{latestErr: sql.ErrNoRows}
	scheduler := NewScheduler(Config{
		Source: staticSource{snapshot: definitions.Snapshot{Loops: []domain.Loop{testLoop("loop-a", false, "0 9 * * *")}}},
		Store:  store,
		Clock:  fixedClock{now: now},
	})

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.fires) != 0 || len(store.goals) != 0 {
		t.Fatalf("created fires=%#v goals=%#v for inactive loop", store.fires, store.goals)
	}
}

func TestTickDoesNotDuplicateFireForSameScheduledMinute(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	store := &fakeStore{latestErr: sql.ErrNoRows}
	scheduler := NewScheduler(Config{
		Source: staticSource{snapshot: definitions.Snapshot{Loops: []domain.Loop{testLoop("loop-a", true, "0 9 * * *")}}},
		Store:  store,
		Clock:  fixedClock{now: now},
	})

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.fires) != 1 {
		t.Fatalf("fires = %#v, want one", store.fires)
	}
	if len(store.goals) != 1 {
		t.Fatalf("goals = %#v, want one", store.goals)
	}
}

func TestRunContinuesAfterDefinitionsLoadFailure(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	store := &fakeStore{latestErr: sql.ErrNoRows, onCreateFire: cancel}
	scheduler := NewScheduler(Config{
		Source: &sequenceSource{results: []sourceResult{
			{err: errors.New("bad definitions")},
			{snapshot: definitions.Snapshot{Loops: []domain.Loop{testLoop("loop-a", true, "0 9 * * *")}}},
		}},
		Store:        store,
		Clock:        fixedClock{now: now},
		PollInterval: time.Millisecond,
	})

	if err := scheduler.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(store.fires) != 1 {
		t.Fatalf("fires = %#v, want one after retry", store.fires)
	}
}

func testLoop(id string, active bool, cron string) domain.Loop {
	return domain.Loop{
		ID:          id,
		PatternID:   "pattern-a",
		Active:      active,
		Cron:        cron,
		Autonomy:    domain.LoopAutonomyL1,
		Trigger:     domain.LoopTriggerPiramid,
		Goal:        "Maintain project",
		ProjectPath: "/tmp/project",
		HumanGates:  []string{"pre_execution"},
		Token:       domain.LoopToken{DailyCap: 1000},
	}
}

type staticSource struct {
	snapshot definitions.Snapshot
	err      error
}

func (s staticSource) Load(context.Context) (definitions.Snapshot, error) {
	return s.snapshot, s.err
}

type sourceResult struct {
	snapshot definitions.Snapshot
	err      error
}

type sequenceSource struct {
	results []sourceResult
	index   int
}

func (s *sequenceSource) Load(context.Context) (definitions.Snapshot, error) {
	if s.index >= len(s.results) {
		return s.results[len(s.results)-1].snapshot, s.results[len(s.results)-1].err
	}
	result := s.results[s.index]
	s.index++
	return result.snapshot, result.err
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fakeStore struct {
	latest       domain.Fire
	latestErr    error
	fires        []domain.Fire
	goals        []domain.Goal
	onCreateFire func()
}

func (s *fakeStore) GetLatestFireByLoop(context.Context, string) (domain.Fire, error) {
	return s.latest, s.latestErr
}

func (s *fakeStore) CreateFire(_ context.Context, fire domain.Fire) (domain.Fire, error) {
	s.fires = append(s.fires, fire)
	s.latest = fire
	s.latestErr = nil
	if s.onCreateFire != nil {
		s.onCreateFire()
	}
	return fire, nil
}

func (s *fakeStore) SaveGoalDraft(_ context.Context, goal domain.Goal, _ storepkg.PersistedPaths) error {
	s.goals = append(s.goals, goal)
	return nil
}
