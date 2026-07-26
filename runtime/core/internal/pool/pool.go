// Package pool provides the shared worker pool a flow owns and hands to any block
// that needs parallelism (e.g. a fork running its branches concurrently). Simple
// flows stay single-threaded while concurrent composites schedule work without
// each owning their own goroutines.
package pool

import (
	"fmt"
	"sync"
)

const (
	defaultWorkers = 8
	defaultQueue   = 64
)

// Pool runs tasks on a fixed set of worker goroutines reading a bounded queue.
type Pool struct {
	workers int
	tasks   chan func()
	wg      sync.WaitGroup

	// mu guards closed and, with it, the send in SubmitWait: a submitter holds it
	// for read across the send, so Stop cannot close the queue underneath one.
	mu     sync.RWMutex
	closed bool
}

// New builds a pool with a bounded task queue. workers and queue are clamped to
// their defaults when non-positive.
func New(workers, queue int) *Pool {
	if workers <= 0 {
		workers = defaultWorkers
	}
	if queue <= 0 {
		queue = defaultQueue
	}
	return &Pool{
		workers: workers,
		tasks:   make(chan func(), queue),
	}
}

// Start spawns the worker goroutines. They are ready before any task is
// submitted.
func (p *Pool) Start() {
	p.wg.Add(p.workers)
	for i := 0; i < p.workers; i++ {
		go p.worker()
	}
}

// worker runs tasks until the queue is closed and drained.
func (p *Pool) worker() {
	defer p.wg.Done()
	for task := range p.tasks {
		task()
	}
}

// Submit enqueues a task without blocking. A full queue means the pool is
// exhausted; rather than risk a silent deadlock (a caller waiting on a task that
// can never be scheduled), Submit panics. This is a deliberate, documented
// limitation of the current model: size the pool for the flow's fan-out.
func (p *Pool) Submit(task func()) {
	select {
	case p.tasks <- task:
	default:
		panic(fmt.Sprintf("flow pool exhausted: queue of %d full with %d workers", cap(p.tasks), p.workers))
	}
}

// TrySubmit enqueues a task if there is room, reporting whether it did. Where
// Submit treats a full queue as a bug and panics, TrySubmit treats it as the
// expected answer, for a caller that has somewhere else to put the work — see
// the caller-runs policy in the runtime's resume.
//
// Blocking here instead would be the obvious alternative and is a trap: the
// tasks are flows, a flow may itself schedule more work, and a pool whose
// workers are all parked waiting to submit can never drain the queue that would
// release them. Refusing lets the caller run the work itself, which cannot
// deadlock.
//
// It reports false once the pool is closed, so a block's own goroutine that
// outlives the flow by an instant gets an answer rather than a panic.
func (p *Pool) TrySubmit(task func()) bool {
	// Held for read across the send so Stop, which needs the write lock, cannot
	// close the queue underneath it.
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return false
	}
	select {
	case p.tasks <- task:
		return true
	default:
		return false
	}
}

// Stop closes the task queue and waits for in-flight tasks to finish. It must be
// called only after every submitter has stopped (i.e. after the outer workers
// drain); the closed flag only makes a late submitter fail cleanly rather than
// panic, it does not make Stop safe to call early.
func (p *Pool) Stop() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.tasks)
	p.mu.Unlock()

	p.wg.Wait()
}
