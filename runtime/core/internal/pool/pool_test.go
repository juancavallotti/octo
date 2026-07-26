package pool

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestTrySubmitRefusesAFullQueue(t *testing.T) {
	p := New(1, 1)

	// No workers started, so the one slot fills and stays full. Refusing is the
	// answer that lets a caller run the work itself; blocking here is what would
	// deadlock a pool whose own tasks schedule more work.
	if !p.TrySubmit(func() {}) {
		t.Fatal("the first task should fit")
	}
	if p.TrySubmit(func() {}) {
		t.Fatal("a full queue should refuse rather than accept or block")
	}
}

func TestTrySubmitRunsEveryAcceptedTask(t *testing.T) {
	p := New(2, 8)
	p.Start()

	const n = 100
	var (
		ran      atomic.Int64
		accepted int
	)
	var done sync.WaitGroup
	for i := 0; i < n; i++ {
		// Counted before the submit, not after: an accepted task can be picked up
		// and finished by a worker before TrySubmit has even returned.
		done.Add(1)
		if p.TrySubmit(func() { defer done.Done(); ran.Add(1) }) {
			accepted++
			continue
		}
		done.Done()
	}
	done.Wait()
	p.Stop()

	if got := ran.Load(); got != int64(accepted) {
		t.Errorf("ran %d of %d accepted tasks", got, accepted)
	}
}

func TestTrySubmitAfterStopRefuses(t *testing.T) {
	p := New(1, 4)
	p.Start()
	p.Stop()

	// A block's own goroutine can outlive the flow by an instant; it should get an
	// answer rather than a panic on a closed channel.
	if p.TrySubmit(func() {}) {
		t.Fatal("a stopped pool should refuse work")
	}
}

func TestStopIsSafeWhileSubmittingConcurrently(t *testing.T) {
	p := New(1, 1)
	p.Start()

	// Hammer the pool from several goroutines while it is being stopped: no call
	// may panic, whatever it returns.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				p.TrySubmit(func() {})
			}
		}()
	}
	time.Sleep(5 * time.Millisecond)
	p.Stop()
	wg.Wait()
}
