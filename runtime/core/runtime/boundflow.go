package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/core/internal/engine"
	"github.com/juancavallotti/eip-go/core/internal/pool"
	"github.com/juancavallotti/eip-go/types"
)

const (
	defaultWorkers = 1
	defaultBuffer  = 64
)

// boundFlow is a runnable top-level pipeline: a source feeding a root Flow, run
// by a dedicated pool of workers all reading the same channel. It is assembled
// by the Service.
type boundFlow struct {
	name   string
	source core.MessageSource
	root   *engine.Flow
	// errorPath is the flow-level error chain, run when root fails. It is nil when
	// the flow declares no error path. On success its output becomes the result
	// (recovery); the failing error is exposed to it as vars.error.
	errorPath *engine.Flow
	workers   int
	in        chan *types.Message
	bus       *core.EventBus
	pool      *pool.Pool
	wg        sync.WaitGroup
	// implicit is true when the flow is driven by an implicit source (it is
	// callable by name and acquires no external resources). Implicit flows start
	// before source-backed ones so they are registered before any real source
	// begins admitting traffic that may flow-ref them.
	implicit bool
	// sourceDesc is a human-readable description of the flow's source, for logs.
	sourceDesc string
}

// resolveWorkers returns the configured worker count or the default.
func resolveWorkers(configured int) int {
	if configured > 0 {
		return configured
	}
	return defaultWorkers
}

// resolveBuffer returns the configured channel depth or the default.
func resolveBuffer(configured int) int {
	if configured > 0 {
		return configured
	}
	return defaultBuffer
}

// start starts the shared pool, spawns the worker pool, and then starts the
// source. The pool and workers are ready before any message is produced.
func (bf *boundFlow) start(ctx context.Context) error {
	bf.pool.Start()
	bf.wg.Add(bf.workers)
	for i := 0; i < bf.workers; i++ {
		go bf.worker(ctx)
	}
	return bf.source.Start(ctx)
}

// stop stops the source, closes the channel, drains in-flight messages, then
// stops the shared pool. The pool is torn down last so no worker submits to a
// closed pool.
func (bf *boundFlow) stop(ctx context.Context) error {
	if err := bf.source.Stop(ctx); err != nil {
		return err
	}
	close(bf.in)
	bf.wg.Wait()
	bf.pool.Stop()
	return nil
}

// worker processes messages until the channel is closed and drained.
func (bf *boundFlow) worker(ctx context.Context) {
	defer bf.wg.Done()
	for msg := range bf.in {
		bf.handle(ctx, msg)
	}
}

// handle runs one message through the root flow and publishes its outcome. All
// events key on the inbound EventID (stable for the message's life, so it equals
// out.EventID) to keep correlation consistent for request/response sources.
func (bf *boundFlow) handle(ctx context.Context, msg *types.Message) {
	bf.publish(types.FlowEventStarted, msg.EventID, nil, nil)

	out, err := bf.root.Process(ctx, msg)
	if err != nil && bf.errorPath != nil {
		// Recovery: expose the failure as vars.error and run the error path. On
		// success its output replaces the result; if it also fails, that error
		// stands and the flow is reported failed.
		//
		// Error handling is optional: a flow with no error path (errorPath nil)
		// behaves like an empty error handler — the error propagates and the flow
		// is reported failed, the default below.
		engine.SetErrorVariable(msg, bf.name, err)
		if recovered, altErr := bf.errorPath.Process(ctx, msg); altErr != nil {
			err = fmt.Errorf("error path: %w", altErr)
		} else {
			out, err = recovered, nil
		}
	}
	switch {
	case err != nil:
		slog.Error("flow processing failed", "flow", bf.name, "event_id", msg.EventID, "error", err)
		bf.publish(types.FlowEventFailed, msg.EventID, err, msg)
	case out == nil:
		bf.publish(types.FlowEventDropped, msg.EventID, nil, msg)
	default:
		bf.publish(types.FlowEventCompleted, msg.EventID, nil, out)
	}
}

// publish emits a flow event if a bus is configured. result is the message to
// attach to the event (nil for the started event).
func (bf *boundFlow) publish(kind types.FlowEventKind, eventID string, err error, result *types.Message) {
	if bf.bus == nil {
		return
	}
	bf.bus.Publish(types.FlowEvent{
		Kind:       kind,
		Flow:       bf.name,
		EventID:    eventID,
		OccurredAt: time.Now(),
		Err:        err,
		Result:     result,
	})
}
