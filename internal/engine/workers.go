package engine

import (
	"fmt"
	"sync"
)

type WorkerPool struct {
	available chan string
	active    map[string]struct{}
	mu        sync.Mutex
}

func NewWorkerPool(count int) *WorkerPool {
	if count < 1 {
		count = 1
	}
	pool := &WorkerPool{
		available: make(chan string, count),
		active:    make(map[string]struct{}, count),
	}
	for index := 1; index <= count; index++ {
		pool.available <- fmt.Sprintf("pi-worker-%02d", index)
	}
	return pool
}

func (p *WorkerPool) Acquire() (string, bool) {
	select {
	case workerID := <-p.available:
		p.mu.Lock()
		p.active[workerID] = struct{}{}
		p.mu.Unlock()
		return workerID, true
	default:
		return "", false
	}
}

func (p *WorkerPool) Release(workerID string) {
	p.mu.Lock()
	if _, exists := p.active[workerID]; !exists {
		p.mu.Unlock()
		return
	}
	delete(p.active, workerID)
	p.mu.Unlock()
	p.available <- workerID
}

func (p *WorkerPool) Available() int {
	return len(p.available)
}
