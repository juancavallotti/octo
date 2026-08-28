package api

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Bounds on the poll loops' own backoff, applied when the platform cannot be
// reached at all. The loop is the retry — the receive route is deliberately not
// retried by the client, because a retried long poll would double the outstanding
// connections rather than the effects.
const (
	pollBackoffBase = 200 * time.Millisecond
	pollBackoffCap  = 5 * time.Second
	// defaultPollTimeout is how long a receive waits for a message before the
	// server answers 204 and the loop asks again.
	defaultPollTimeout = 20 * time.Second
	// defaultMaxBatch is how many messages one receive may return. Batching is
	// what creates concurrency here: one poll goroutine per subscription feeds
	// every worker, so the platform sees one long-poll connection per
	// subscription rather than one per listener.
	defaultMaxBatch = 8
	maxAllowedBatch = 1000
	// pollTimeoutHeadroom is how much longer than the declared poll timeout the
	// client's own deadline is, so an expiring long poll is answered rather than
	// cut off.
	pollTimeoutHeadroom = 5 * time.Second
)

// pollConfig is the resolved receive-loop configuration for one plane.
type pollConfig struct {
	timeout  time.Duration
	maxBatch int
}

// resolvePoll clamps what the platform declared into something workable, logging
// once when it has to. A declared value that cannot be honoured is a
// misconfiguration worth naming, but not one worth refusing to start over.
func resolvePoll(plane string, timeoutSeconds, maxBatch int, longTimeout time.Duration) pollConfig {
	cfg := pollConfig{
		timeout:  orDefault(timeoutSeconds, defaultPollTimeout),
		maxBatch: defaultMaxBatch,
	}
	if maxBatch > 0 {
		cfg.maxBatch = maxBatch
	}
	if cfg.maxBatch > maxAllowedBatch {
		slog.Warn("api: the platform API declared an unworkable batch size; clamping",
			"plane", plane, "declared", cfg.maxBatch, "using", maxAllowedBatch)
		cfg.maxBatch = maxAllowedBatch
	}
	// The client has to outwait the server, or every long poll ends as a timeout
	// on this side and the 204 that says "nothing yet" is never seen.
	if ceiling := longTimeout - pollTimeoutHeadroom; ceiling > 0 && cfg.timeout > ceiling {
		slog.Warn("api: the platform API's poll timeout exceeds this runtime's long-request "+
			"timeout; shortening the poll so an empty poll is answered rather than cut off",
			"plane", plane, "declared", cfg.timeout, "using", ceiling, "var", envLongTimeout)
		cfg.timeout = ceiling
	}
	return cfg
}

// pollLoop runs one subscription: a single goroutine asking for messages, and a
// pool of workers running the handler.
//
// One poll goroutine rather than one per listener is the whole design. maxBatch
// is what creates the concurrency, so the platform sees a single long-poll
// connection per subscription — eight would look like eight idle clients to
// anything counting connections, which on Cloud Run is what decides how many
// instances stay up.
type pollLoop struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
}

// run starts the loop. receive fetches a batch (returning nothing on an empty
// poll), and handle processes one item. window is how long a receive was asked to
// wait, used to pace the loop against a server that does not actually hold the
// request open.
func run[T any](
	ctx context.Context,
	workers int,
	window time.Duration,
	receive func(context.Context) ([]T, error),
	handle func(context.Context, T),
) *pollLoop {
	loopCtx, cancel := context.WithCancel(ctx)
	l := &pollLoop{cancel: cancel}

	work := make(chan T, workers)
	l.wg.Add(workers)
	for range workers {
		go func() {
			defer l.wg.Done()
			for item := range work {
				handle(loopCtx, item)
			}
		}()
	}

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		defer close(work)
		poll(loopCtx, window, receive, work)
	}()
	return l
}

// poll asks for messages until the context ends, backing off when the platform
// cannot be reached and pacing itself when the platform answers too fast.
func poll[T any](
	ctx context.Context, window time.Duration,
	receive func(context.Context) ([]T, error), work chan<- T,
) {
	delay := pollBackoffBase
	for ctx.Err() == nil {
		started := time.Now()
		items, err := receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !backOff(ctx, &delay, err) {
				return
			}
			continue
		}
		delay = pollBackoffBase
		if len(items) == 0 {
			// A server that answers an empty poll immediately rather than holding
			// the request open is a common and easy implementation mistake, and
			// without this the loop would spin against it as fast as the network
			// allows. Waiting out the rest of the window this poll asked for gives
			// the same pacing a correct long poll would have.
			if err := sleep(ctx, window-time.Since(started)); err != nil {
				return
			}
			continue
		}
		for _, item := range items {
			select {
			case work <- item:
			case <-ctx.Done():
				return
			}
		}
	}
}

// backOff waits out a failed receive and widens the delay, reporting whether the
// loop should keep going.
//
// It logs once per transition rather than per attempt: a platform that is down
// for a minute should not write a minute of identical lines.
func backOff(ctx context.Context, delay *time.Duration, cause error) bool {
	if *delay == pollBackoffBase {
		slog.Warn("api: a receive call failed; backing off", "error", cause)
	}
	if err := sleep(ctx, jitter(*delay)); err != nil {
		return false
	}
	if *delay *= 2; *delay > pollBackoffCap {
		*delay = pollBackoffCap
	}
	return true
}

// close stops the loop and waits for every worker, so no handler is still running
// when it returns.
func (l *pollLoop) close() {
	l.once.Do(func() {
		l.cancel()
		l.wg.Wait()
	})
}
