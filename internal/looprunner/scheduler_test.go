package looprunner

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/definitions"
	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/home"
	"github.com/heruujoko/piramid/internal/intake"
	"github.com/heruujoko/piramid/internal/records"
	storepkg "github.com/heruujoko/piramid/internal/store"
	"github.com/heruujoko/piramid/internal/store/sqlite"
)

func TestTickLogsLifecycleAndFireCreation(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	logger := &captureLogger{}
	scheduler := NewScheduler(Config{
		Source: staticSource{snapshot: definitions.Snapshot{Loops: []domain.Loop{testLoop("loop-a", true, "0 9 * * *")}}},
		Store:  &fakeStore{latestErr: sql.ErrNoRows},
		Clock:  fixedClock{now: now},
		Logger: logger,
	})

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	logger.requireContains(t, "loop scheduler tick started")
	logger.requireContains(t, "created fire")
	logger.requireContains(t, "loop scheduler tick completed")
}

func TestTickLogsDefinitionLoadFailures(t *testing.T) {
	logger := &captureLogger{}
	loadErr := errors.New("bad definitions")
	scheduler := NewScheduler(Config{
		Source: staticSource{err: loadErr},
		Store:  &fakeStore{},
		Clock:  fixedClock{now: time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)},
		Logger: logger,
	})

	if err := scheduler.Tick(context.Background()); !errors.Is(err, loadErr) {
		t.Fatalf("err = %v, want %v", err, loadErr)
	}
	logger.requireContains(t, "failed to load loop definitions")
}

func TestTickLogsStoreErrors(t *testing.T) {
	logger := &captureLogger{}
	storeErr := errors.New("store unavailable")
	scheduler := NewScheduler(Config{
		Source: staticSource{snapshot: definitions.Snapshot{Loops: []domain.Loop{testLoop("loop-a", true, "0 9 * * *")}}},
		Store:  &fakeStore{latestErr: sql.ErrNoRows, saveErr: storeErr},
		Clock:  fixedClock{now: time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)},
		Logger: logger,
	})

	if err := scheduler.Tick(context.Background()); !errors.Is(err, storeErr) {
		t.Fatalf("err = %v, want %v", err, storeErr)
	}
	logger.requireContains(t, "loop tick failed")
}

func TestTickCreatesConfirmableDraftGoal(t *testing.T) {
	recordStore := records.New(home.NewPaths(t.TempDir()))
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	scheduler := NewScheduler(Config{
		Source:  staticSource{snapshot: definitions.Snapshot{Loops: []domain.Loop{testLoop("loop-a", true, "0 9 * * *")}}},
		Store:   st,
		Clock:   fixedClock{now: now},
		Records: recordStore,
	})
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	fires, err := st.ListFires(context.Background(), "loop-a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 1 {
		t.Fatalf("fires = %#v, want one", fires)
	}
	service := intake.NewService(intake.ServiceConfig{
		Repository: st,
		Records:    recordStore,
		Now:        func() time.Time { return now },
	})
	if err := service.Confirm(context.Background(), fires[0].GoalID); err != nil {
		t.Fatal(err)
	}
}

func TestTickPersistsDueLoopWithSQLiteForeignKeys(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	scheduler := NewScheduler(Config{
		Source: staticSource{snapshot: definitions.Snapshot{Loops: []domain.Loop{testLoop("loop-a", true, "0 9 * * *")}}},
		Store:  st,
		Clock:  fixedClock{now: now},
	})

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	fires, err := st.ListFires(context.Background(), "loop-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 1 || fires[0].GoalID == "" {
		t.Fatalf("fires = %#v, want one linked fire", fires)
	}
}

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

func TestTickGeneratesDistinctIDsForSimilarLoopIDs(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	store := &fakeStore{latestErr: sql.ErrNoRows}
	scheduler := NewScheduler(Config{
		Source: staticSource{snapshot: definitions.Snapshot{Loops: []domain.Loop{
			testLoop("a_b", true, "0 9 * * *"),
			testLoop("a-b", true, "0 9 * * *"),
		}}},
		Store: store,
		Clock: fixedClock{now: now},
	})

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.fires) != 2 || len(store.goals) != 2 {
		t.Fatalf("fires=%#v goals=%#v, want one of each per loop", store.fires, store.goals)
	}
	if store.fires[0].ID == store.fires[1].ID {
		t.Fatalf("fire IDs collided: %q", store.fires[0].ID)
	}
	if store.goals[0].ID == store.goals[1].ID {
		t.Fatalf("goal IDs collided: %q", store.goals[0].ID)
	}
}

func TestTickCanRetryFireCreationAfterAtomicFailure(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	seedGoal := domain.Goal{ID: "GOAL-SEED", Text: "seed", ProjectPath: "/tmp/project", Status: domain.GoalDraft, CreatedAt: now}
	if err := st.SaveGoalDraft(context.Background(), seedGoal, storepkg.PersistedPaths{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateFire(context.Background(), domain.Fire{
		ID:          "FIRE-DUP",
		LoopID:      "other-loop",
		GoalID:      seedGoal.ID,
		Status:      domain.FireScheduled,
		ScheduledAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}

	first := NewScheduler(Config{
		Source: staticSource{snapshot: definitions.Snapshot{Loops: []domain.Loop{testLoop("loop-a", true, "0 9 * * *")}}},
		Store:  st,
		Clock:  fixedClock{now: now},
		IDGenerator: func(prefix string, _ time.Time, _ string) string {
			if prefix == "FIRE" {
				return "FIRE-DUP"
			}
			return "GOAL-NEW"
		},
	})
	if err := first.Tick(context.Background()); err == nil {
		t.Fatal("first tick succeeded, want duplicate fire failure")
	}

	second := NewScheduler(Config{
		Source: staticSource{snapshot: definitions.Snapshot{Loops: []domain.Loop{testLoop("loop-a", true, "0 9 * * *")}}},
		Store:  st,
		Clock:  fixedClock{now: now},
		IDGenerator: func(prefix string, _ time.Time, _ string) string {
			if prefix == "FIRE" {
				return "FIRE-OK"
			}
			return "GOAL-NEW"
		},
	})
	if err := second.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	fires, err := st.ListFires(context.Background(), "loop-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 1 || fires[0].ID != "FIRE-OK" || fires[0].GoalID != "GOAL-NEW" {
		t.Fatalf("fires = %#v, want retry to create FIRE-OK linked to GOAL-NEW", fires)
	}
}

func TestTickContinuesAfterSingleLoopError(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	storeErr := errors.New("loop-a latest failed")
	store := &fakeStore{
		latestErr:       sql.ErrNoRows,
		latestErrByLoop: map[string]error{"loop-a": storeErr},
	}
	scheduler := NewScheduler(Config{
		Source: staticSource{snapshot: definitions.Snapshot{Loops: []domain.Loop{
			testLoop("loop-a", true, "0 9 * * *"),
			testLoop("loop-b", true, "0 9 * * *"),
		}}},
		Store: store,
		Clock: fixedClock{now: now},
	})

	err := scheduler.Tick(context.Background())
	if !errors.Is(err, storeErr) {
		t.Fatalf("err = %v, want %v", err, storeErr)
	}
	if len(store.fires) != 1 || store.fires[0].LoopID != "loop-b" {
		t.Fatalf("fires = %#v, want loop-b to fire despite loop-a error", store.fires)
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

func TestRunReportsTickErrors(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	storeErr := errors.New("store unavailable")
	var reported []error
	scheduler := NewScheduler(Config{
		Source: staticSource{snapshot: definitions.Snapshot{Loops: []domain.Loop{testLoop("loop-a", true, "0 9 * * *")}}},
		Store:  &fakeStore{latestErr: sql.ErrNoRows, saveErr: storeErr},
		Clock:  fixedClock{now: now},
		OnError: func(err error) {
			reported = append(reported, err)
			cancel()
		},
	})

	if err := scheduler.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(reported) != 1 || !errors.Is(reported[0], storeErr) {
		t.Fatalf("reported errors = %#v, want %v", reported, storeErr)
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

type captureLogger struct {
	messages []string
}

func (l *captureLogger) Printf(format string, args ...any) {
	l.messages = append(l.messages, format)
}

func (l *captureLogger) requireContains(t *testing.T, want string) {
	t.Helper()
	for _, message := range l.messages {
		if message == want {
			return
		}
	}
	t.Fatalf("logs = %#v, want message %q", l.messages, want)
}

type fakeStore struct {
	latest          domain.Fire
	latestByLoop    map[string]domain.Fire
	latestErr       error
	latestErrByLoop map[string]error
	fires           []domain.Fire
	goals           []domain.Goal
	saveErr         error
	onCreateFire    func()
}

func (s *fakeStore) GetLatestFireByLoop(_ context.Context, loopID string) (domain.Fire, error) {
	if err, ok := s.latestErrByLoop[loopID]; ok {
		return domain.Fire{}, err
	}
	if s.latestByLoop != nil {
		if fire, ok := s.latestByLoop[loopID]; ok {
			return fire, nil
		}
		return domain.Fire{}, sql.ErrNoRows
	}
	return s.latest, s.latestErr
}

func (s *fakeStore) CreateFire(_ context.Context, fire domain.Fire) (domain.Fire, error) {
	s.fires = append(s.fires, fire)
	if s.latestByLoop == nil {
		s.latestByLoop = make(map[string]domain.Fire)
	}
	s.latestByLoop[fire.LoopID] = fire
	s.latest = fire
	s.latestErr = nil
	if s.onCreateFire != nil {
		s.onCreateFire()
	}
	return fire, nil
}

func (s *fakeStore) SaveGoalDraft(_ context.Context, goal domain.Goal, _ storepkg.PersistedPaths) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.goals = append(s.goals, goal)
	return nil
}
