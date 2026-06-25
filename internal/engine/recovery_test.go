package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/home"
	storepkg "github.com/heruujoko/piramid/internal/store"
	sqlitestore "github.com/heruujoko/piramid/internal/store/sqlite"
)

type fakeProcessInspector struct {
	live map[int]bool
}

func (f fakeProcessInspector) Exists(pid int) bool {
	return f.live[pid]
}

type failingRecoveryStore struct{}

func (failingRecoveryStore) ListActiveAttempts(context.Context) ([]storepkg.InterruptedAttempt, error) {
	return nil, errors.New("database unavailable")
}
func (failingRecoveryStore) RecoverActive(context.Context, time.Time) ([]storepkg.InterruptedAttempt, error) {
	return nil, errors.New("database unavailable")
}

func recoveryStore(t *testing.T) (*sqlitestore.Store, string) {
	t.Helper()
	paths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	if err := home.Init(paths); err != nil {
		t.Fatal(err)
	}
	st, err := sqlitestore.Open(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	task := domain.Task{
		ID: "TASK-1", Goal: "work", ProjectPath: project,
		DOD: []string{"done"}, MaxAttempts: 3, Timeout: time.Hour, TimeoutText: "1h",
	}
	goal := domain.Goal{
		ID: "GOAL-1", Text: "work", ProjectPath: project,
		Status: domain.GoalConfirmed, CreatedAt: time.Now().UTC(),
	}
	if err := st.AdmitPlan(context.Background(), goal,
		domain.Plan{Version: 1, GoalID: goal.ID, Tasks: []domain.Task{task}},
		storepkg.PersistedPaths{}); err != nil {
		t.Fatal(err)
	}
	return st, project
}

func TestRecoverInterruptsOrphanedAttemptAndReleasesLease(t *testing.T) {
	st, project := recoveryStore(t)
	attempt, err := st.StartAttempt(context.Background(), storepkg.StartAttemptInput{
		TaskID: "TASK-1", WorkerID: "worker-1", Runtime: "pi-cli", StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 25, 5, 0, 0, 0, time.UTC)

	if err := Recover(context.Background(), st, fakeProcessInspector{}, now); err != nil {
		t.Fatal(err)
	}
	task, err := st.GetTask(context.Background(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.TaskPending {
		t.Fatalf("task status = %s, want PENDING", task.Status)
	}
	if len(task.Attempts) != 1 || task.Attempts[0].Status != domain.AttemptInterrupted {
		t.Fatalf("attempts = %#v", task.Attempts)
	}
	if task.Attempts[0].ID != attempt.ID {
		t.Fatalf("attempt id = %d", task.Attempts[0].ID)
	}
	if err := st.AcquireReadLease(context.Background(), project, "planner"); err != nil {
		t.Fatalf("stale write lease remained: %v", err)
	}
}

func TestRecoverRefusesToDuplicateLiveProcess(t *testing.T) {
	st, _ := recoveryStore(t)
	attempt, err := st.StartAttempt(context.Background(), storepkg.StartAttemptInput{
		TaskID: "TASK-1", WorkerID: "worker-1", Runtime: "pi-cli", StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MoveToVerification(context.Background(), storepkg.FinishExecutionInput{
		TaskID: "TASK-1", AttemptID: attempt.ID, ProcessID: 4242,
		ExitCode: 0, FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	err = Recover(context.Background(), st, fakeProcessInspector{live: map[int]bool{4242: true}}, time.Now())
	if err == nil {
		t.Fatal("Recover() accepted live orphan process")
	}
	task, getErr := st.GetTask(context.Background(), "TASK-1")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if task.Status != domain.TaskVerifying {
		t.Fatalf("status changed to %s", task.Status)
	}
}

func TestRecoverReleasesHistoricalStaleLease(t *testing.T) {
	st, project := recoveryStore(t)
	if err := st.AcquireReadLease(context.Background(), project, "stale-planner"); err != nil {
		t.Fatal(err)
	}
	if err := Recover(context.Background(), st, fakeProcessInspector{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := st.AcquireReadLease(context.Background(), project, "new-planner"); err != nil {
		t.Fatalf("stale read lease remained: %v", err)
	}
}

func TestRecoverReturnsPersistenceFailure(t *testing.T) {
	err := Recover(context.Background(), failingRecoveryStore{}, fakeProcessInspector{}, time.Now())
	if err == nil || !errors.Is(err, errors.New("database unavailable")) &&
		err.Error() != "list active attempts: database unavailable" {
		t.Fatalf("Recover() error = %v", err)
	}
}
