package pool

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestPoolRunsEveryTaskOnce(t *testing.T) {
	tests := []struct {
		name    string
		workers int
		queue   int
		tasks   int
	}{
		{name: "single worker", workers: 1, queue: 16, tasks: 16},
		{name: "many workers", workers: 8, queue: 64, tasks: 64},
		{name: "defaults applied", workers: 0, queue: 0, tasks: 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.workers, tt.queue)
			p.Start()

			var (
				ran  atomic.Int64
				done sync.WaitGroup
			)
			done.Add(tt.tasks)
			for i := 0; i < tt.tasks; i++ {
				p.Submit(func() {
					ran.Add(1)
					done.Done()
				})
			}
			done.Wait()
			p.Stop()

			if got := ran.Load(); got != int64(tt.tasks) {
				t.Errorf("ran %d tasks, want %d", got, tt.tasks)
			}
		})
	}
}

func TestPoolStopDrainsInFlight(t *testing.T) {
	p := New(2, 64)
	p.Start()

	const n = 50
	var ran atomic.Int64
	for i := 0; i < n; i++ {
		p.Submit(func() { ran.Add(1) })
	}

	// Stop must not return until every submitted task has run.
	p.Stop()

	if got := ran.Load(); got != n {
		t.Errorf("Stop returned with %d tasks run, want %d", got, n)
	}
}

func TestPoolPanicsWhenExhausted(t *testing.T) {
	// A pool with no started workers and a zero-capacity-after-fill queue cannot
	// accept further work, so Submit must panic rather than block forever.
	p := New(1, 1)
	// Do not start workers, so the single queue slot fills and never drains.
	p.Submit(func() { select {} }) // fills the only slot

	defer func() {
		if r := recover(); r == nil {
			t.Error("Submit on an exhausted pool did not panic")
		}
	}()
	p.Submit(func() {})
}
