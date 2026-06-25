package engine

import (
	"context"
	"sort"
	"sync"

	"github.com/heruujoko/piramid/internal/app"
)

type Supervisor struct {
	runner  *Runner
	mu      sync.Mutex
	workers map[string]app.WorkerView
	cancels map[string]context.CancelFunc
	wg      sync.WaitGroup
}

func NewSupervisor(runner *Runner) *Supervisor {
	return &Supervisor{
		runner: runner, workers: map[string]app.WorkerView{},
		cancels: map[string]context.CancelFunc{},
	}
}

func (s *Supervisor) Run(ctx context.Context, dispatches <-chan Dispatch) {
	for {
		select {
		case <-ctx.Done():
			s.cancelAll()
			s.wg.Wait()
			return
		case dispatch, ok := <-dispatches:
			if !ok {
				s.cancelAll()
				s.wg.Wait()
				return
			}
			s.start(ctx, dispatch)
		}
	}
}

func (s *Supervisor) start(parent context.Context, dispatch Dispatch) {
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	s.workers[dispatch.WorkerID] = app.WorkerView{
		ID: dispatch.WorkerID, Status: "running",
		TaskID: dispatch.Task.ID, AttemptID: dispatch.Attempt.ID,
	}
	s.cancels[dispatch.Task.ID] = cancel
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		_ = s.runner.Run(ctx, dispatch)
		s.mu.Lock()
		delete(s.workers, dispatch.WorkerID)
		delete(s.cancels, dispatch.Task.ID)
		s.mu.Unlock()
	}()
}

func (s *Supervisor) CancelTask(taskID string) {
	s.mu.Lock()
	cancel := s.cancels[taskID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Supervisor) ListWorkers(context.Context) ([]app.WorkerView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workers := make([]app.WorkerView, 0, len(s.workers))
	for _, worker := range s.workers {
		workers = append(workers, worker)
	}
	sort.Slice(workers, func(i, j int) bool { return workers[i].ID < workers[j].ID })
	return workers, nil
}

func (s *Supervisor) cancelAll() {
	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.cancels))
	for _, cancel := range s.cancels {
		cancels = append(cancels, cancel)
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}
