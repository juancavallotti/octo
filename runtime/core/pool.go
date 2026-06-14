package core

import (
	"fmt"
	"sync"
)

const (
	defaultPoolWorkers = 8
	defaultPoolQueue   = 64
)

// pool runs tasks on a fixed set of worker goroutines reading a bounded queue.
// It is owned by the main flow and shared by any block that needs parallelism
// (e.g. a fork running its branches concurrently), so simple flows stay
// single-threaded while concurrent composites schedule work without each owning
// its own goroutines.
type pool struct {
	workers int
	tasks   chan func()
	wg      sync.WaitGroup
}

// resolvePoolWorkers returns the configured worker count or the default.
func resolvePoolWorkers(configured int) int {
	if configured > 0 {
		return configured
	}
	return defaultPoolWorkers
}

// newPool builds a pool with a bounded task queue. workers and queue are clamped
// to their defaults when non-positive.
func newPool(workers, queue int) *pool {
	if workers <= 0 {
		workers = defaultPoolWorkers
	}
	if queue <= 0 {
		queue = defaultPoolQueue
	}
	return &pool{
		workers: workers,
		tasks:   make(chan func(), queue),
	}
}

// start spawns the worker goroutines. They are ready before any task is
// submitted.
func (p *pool) start() {
	p.wg.Add(p.workers)
	for i := 0; i < p.workers; i++ {
		go p.worker()
	}
}

// worker runs tasks until the queue is closed and drained.
func (p *pool) worker() {
	defer p.wg.Done()
	for task := range p.tasks {
		task()
	}
}

// submit enqueues a task without blocking. A full queue means the pool is
// exhausted; rather than risk a silent deadlock (a caller waiting on a task that
// can never be scheduled), submit panics. This is a deliberate, documented
// limitation of the current model: size the pool for the flow's fan-out.
func (p *pool) submit(task func()) {
	select {
	case p.tasks <- task:
	default:
		panic(fmt.Sprintf("flow pool exhausted: queue of %d full with %d workers", cap(p.tasks), p.workers))
	}
}

// stop closes the task queue and waits for in-flight tasks to finish. It must be
// called only after every submitter has stopped (i.e. after the outer workers
// drain).
func (p *pool) stop() {
	close(p.tasks)
	p.wg.Wait()
}
