package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

type fakeClock struct {
	now time.Time
}

func (f fakeClock) Now() time.Time { return f.now }
func (f fakeClock) After(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- f.now
	return ch
}

type schedulerStore struct {
	mu             sync.Mutex
	runnable       []domain.TaskRecord
	started        []string
	blockedCalls   int
	leasedProjects map[string]bool
}

func (s *schedulerStore) ReconcileBlocked(context.Context, time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blockedCalls++
	return 0, nil
}

func (s *schedulerStore) ListRunnable(context.Context, time.Time, int) ([]domain.TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.TaskRecord(nil), s.runnable...), nil
}

func (s *schedulerStore) StartAttempt(_ context.Context, input storepkg.StartAttemptInput) (domain.Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var task domain.TaskRecord
	for _, candidate := range s.runnable {
		if candidate.ID == input.TaskID {
			task = candidate
			break
		}
	}
	if s.leasedProjects == nil {
		s.leasedProjects = map[string]bool{}
	}
	if s.leasedProjects[task.ProjectPath] {
		return domain.Attempt{}, storepkg.ErrProjectLeased
	}
	s.leasedProjects[task.ProjectPath] = true
	s.started = append(s.started, input.TaskID)
	return domain.Attempt{
		ID:            int64(len(s.started)),
		TaskID:        input.TaskID,
		AttemptNumber: task.AttemptCount + 1,
		WorkerID:      input.WorkerID,
		Status:        domain.AttemptRunning,
		StartedAt:     input.StartedAt,
	}, nil
}

func taskRecord(id, project string) domain.TaskRecord {
	return domain.TaskRecord{
		Task: domain.Task{
			ID:          id,
			ProjectPath: project,
			Timeout:     time.Hour,
		},
		Status: domain.TaskPending,
	}
}

func TestDispatchOncePreservesStoreOrderAndWorkerLimit(t *testing.T) {
	st := &schedulerStore{runnable: []domain.TaskRecord{
		taskRecord("TASK-1", "/project/one"),
		taskRecord("TASK-2", "/project/two"),
		taskRecord("TASK-3", "/project/three"),
	}}
	dispatches := make(chan Dispatch, 3)
	scheduler := NewScheduler(SchedulerConfig{
		Store:       st,
		Clock:       fakeClock{now: time.Date(2026, 6, 25, 1, 0, 0, 0, time.UTC)},
		WorkerCount: 2,
		Dispatch:    dispatches,
	})

	count, err := scheduler.DispatchOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("dispatch count = %d, want 2", count)
	}
	first := <-dispatches
	second := <-dispatches
	if first.Task.ID != "TASK-1" || second.Task.ID != "TASK-2" {
		t.Fatalf("dispatch order = %s, %s", first.Task.ID, second.Task.ID)
	}
	first.Done()
	second.Done()
}

func TestDispatchOnceSkipsConflictingProject(t *testing.T) {
	st := &schedulerStore{runnable: []domain.TaskRecord{
		taskRecord("TASK-1", "/project/shared"),
		taskRecord("TASK-2", "/project/shared"),
		taskRecord("TASK-3", "/project/other"),
	}}
	dispatches := make(chan Dispatch, 3)
	scheduler := NewScheduler(SchedulerConfig{
		Store: st, Clock: fakeClock{now: time.Now()}, WorkerCount: 3, Dispatch: dispatches,
	})

	count, err := scheduler.DispatchOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("dispatch count = %d, want 2", count)
	}
	got := []string{(<-dispatches).Task.ID, (<-dispatches).Task.ID}
	if got[0] != "TASK-1" || got[1] != "TASK-3" {
		t.Fatalf("dispatched = %v", got)
	}
}

func TestDispatchOnceReconcilesBlockedDependencies(t *testing.T) {
	st := &schedulerStore{}
	scheduler := NewScheduler(SchedulerConfig{
		Store: st, Clock: fakeClock{now: time.Now()}, WorkerCount: 1,
		Dispatch: make(chan Dispatch, 1),
	})
	if _, err := scheduler.DispatchOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st.blockedCalls != 1 {
		t.Fatalf("blocked reconciliation calls = %d, want 1", st.blockedCalls)
	}
}

func TestDispatchOnceDoesNothingAfterCancellation(t *testing.T) {
	st := &schedulerStore{runnable: []domain.TaskRecord{taskRecord("TASK-1", "/project")}}
	scheduler := NewScheduler(SchedulerConfig{
		Store: st, Clock: fakeClock{now: time.Now()}, WorkerCount: 1,
		Dispatch: make(chan Dispatch, 1),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	count, err := scheduler.DispatchOnce(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if count != 0 || len(st.started) != 0 {
		t.Fatalf("count=%d started=%v", count, st.started)
	}
}

func TestWorkerPoolReleasesSlots(t *testing.T) {
	pool := NewWorkerPool(1)
	workerID, ok := pool.Acquire()
	if !ok || workerID != "pi-worker-01" {
		t.Fatalf("Acquire() = %q, %v", workerID, ok)
	}
	if _, ok := pool.Acquire(); ok {
		t.Fatal("acquired beyond capacity")
	}
	pool.Release(workerID)
	if _, ok := pool.Acquire(); !ok {
		t.Fatal("released slot did not become available")
	}
}
