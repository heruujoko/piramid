package engine

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                                { return time.Now().UTC() }
func (realClock) After(duration time.Duration) <-chan time.Time { return time.After(duration) }

type SchedulerStore interface {
	ReconcileBlocked(context.Context, time.Time) (int, error)
	ListRunnable(context.Context, time.Time, int) ([]domain.TaskRecord, error)
	StartAttempt(context.Context, storepkg.StartAttemptInput) (domain.Attempt, error)
}

type Dispatch struct {
	Task     domain.TaskRecord
	Attempt  domain.Attempt
	WorkerID string
	doneOnce *sync.Once
	release  func()
}

func (d Dispatch) Done() {
	if d.doneOnce != nil {
		d.doneOnce.Do(d.release)
	}
}

type SchedulerConfig struct {
	Store        SchedulerStore
	Clock        Clock
	WorkerCount  int
	Dispatch     chan<- Dispatch
	PollInterval time.Duration
}

type Scheduler struct {
	store        SchedulerStore
	clock        Clock
	workers      *WorkerPool
	dispatch     chan<- Dispatch
	pollInterval time.Duration
}

func NewScheduler(config SchedulerConfig) *Scheduler {
	clock := config.Clock
	if clock == nil {
		clock = realClock{}
	}
	interval := config.PollInterval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	return &Scheduler{
		store:        config.Store,
		clock:        clock,
		workers:      NewWorkerPool(config.WorkerCount),
		dispatch:     config.Dispatch,
		pollInterval: interval,
	}
}

func (s *Scheduler) DispatchOnce(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	now := s.clock.Now()
	if _, err := s.store.ReconcileBlocked(ctx, now); err != nil {
		return 0, err
	}
	available := s.workers.Available()
	if available == 0 {
		return 0, nil
	}
	tasks, err := s.store.ListRunnable(ctx, now, available*4)
	if err != nil {
		return 0, err
	}
	dispatched := 0
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return dispatched, err
		}
		workerID, ok := s.workers.Acquire()
		if !ok {
			break
		}
		attempt, err := s.store.StartAttempt(ctx, storepkg.StartAttemptInput{
			TaskID:    task.ID,
			WorkerID:  workerID,
			Runtime:   "pi-cli",
			StartedAt: now,
		})
		if err != nil {
			s.workers.Release(workerID)
			if errors.Is(err, storepkg.ErrProjectLeased) {
				continue
			}
			return dispatched, err
		}
		once := &sync.Once{}
		dispatch := Dispatch{
			Task:     task,
			Attempt:  attempt,
			WorkerID: workerID,
			doneOnce: once,
			release:  func() { s.workers.Release(workerID) },
		}
		select {
		case s.dispatch <- dispatch:
			dispatched++
		case <-ctx.Done():
			dispatch.Done()
			return dispatched, ctx.Err()
		}
	}
	return dispatched, nil
}

func (s *Scheduler) Run(ctx context.Context) error {
	for {
		if _, err := s.DispatchOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.clock.After(s.pollInterval):
		}
	}
}
