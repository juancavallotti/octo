package core

import (
	"context"
	"log/slog"
	"sync"
	"time"

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
	name    string
	source  MessageSource
	root    *Flow
	workers int
	in      chan *types.Message
	bus     *EventBus
	wg      sync.WaitGroup
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

// start spawns the worker pool and then starts the source. Workers are ready
// before any message is produced.
func (bf *boundFlow) start(ctx context.Context) error {
	bf.wg.Add(bf.workers)
	for i := 0; i < bf.workers; i++ {
		go bf.worker(ctx)
	}
	return bf.source.Start(ctx)
}

// stop stops the source, closes the channel, and drains in-flight messages.
func (bf *boundFlow) stop(ctx context.Context) error {
	if err := bf.source.Stop(ctx); err != nil {
		return err
	}
	close(bf.in)
	bf.wg.Wait()
	return nil
}

// worker processes messages until the channel is closed and drained.
func (bf *boundFlow) worker(ctx context.Context) {
	defer bf.wg.Done()
	for msg := range bf.in {
		bf.handle(ctx, msg)
	}
}

// handle runs one message through the root flow and publishes its outcome.
func (bf *boundFlow) handle(ctx context.Context, msg *types.Message) {
	bf.publish(types.FlowEventStarted, msg.EventID, nil)

	out, err := bf.root.Process(ctx, msg)
	switch {
	case err != nil:
		slog.Error("flow processing failed", "flow", bf.name, "event_id", msg.EventID, "error", err)
		bf.publish(types.FlowEventFailed, msg.EventID, err)
	case out == nil:
		bf.publish(types.FlowEventDropped, msg.EventID, nil)
	default:
		bf.publish(types.FlowEventCompleted, out.EventID, nil)
	}
}

// publish emits a flow event if a bus is configured.
func (bf *boundFlow) publish(kind types.FlowEventKind, eventID string, err error) {
	if bf.bus == nil {
		return
	}
	bf.bus.Publish(types.FlowEvent{
		Kind:       kind,
		Flow:       bf.name,
		EventID:    eventID,
		OccurredAt: time.Now(),
		Err:        err,
	})
}
