// Package reload turns "go look again" into at most one pull at a time.
//
// Triggers arrive in bursts, and handling each independently would mean N
// concurrent fetches of the same bundle and N interleaved writers into the one
// watched directory, which is how a workspace ends up holding half of two
// generations.
//
// So a pull is serialised, and a request arriving while one is in flight sets a
// pending flag rather than queueing behind it. A flag and not a queue: every
// request in a burst wants the same thing, the latest bundle, so collapsing them
// bounds the work at one extra round.
package reload

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/juancavallotti/octo/sidecars/dev/internal/bundle"
	"github.com/juancavallotti/octo/sidecars/dev/internal/workspace"
)

// puller is the bundle client this needs. Declared here, in the consumer, so a test
// drives a fake with no HTTP server; satisfied by *bundle.Client.
type puller interface {
	Fetch(ctx context.Context) (bundle.Bundle, error)
	Expire(ctx context.Context) error
}

// applier is the workspace this writes through. Satisfied by *workspace.Workspace.
type applier interface {
	Apply(definition string, files []workspace.File) (workspace.Report, error)
}

// Outcome says what a Reload call did, so the caller can answer honestly rather
// than implying work it did not do.
type Outcome int

const (
	// OutcomeApplied means this call pulled and wrote the workspace.
	OutcomeApplied Outcome = iota
	// OutcomeCoalesced means a pull was already in flight, so this call marked it to
	// repeat and returned. The reload will happen, but not within this call.
	OutcomeCoalesced
)

// roundTimeout bounds a reload round that runs on behalf of coalesced callers.
// Those callers already received OutcomeCoalesced and hold no request context of
// their own, so the extra round runs on a context detached from whichever caller
// happened to be in flight when they arrived — see Reload.
const roundTimeout = 60 * time.Second

// State is a snapshot of what the coordinator has done, for GET /status.
type State struct {
	// Generation is the marker the orchestrator stamped on the last applied bundle.
	Generation string `json:"generation,omitempty"`
	// LastPull is when a pull last succeeded; zero if none has.
	LastPull time.Time `json:"lastPull,omitzero"`
	// LastError is the last pull or apply failure, cleared by the next success.
	// Retained rather than only logged, so a failure explains a stale workspace long
	// after it happened.
	LastError string `json:"lastError,omitempty"`
	// Pulls counts successful pulls, so "is it reloading at all?" is answerable.
	Pulls int `json:"pulls"`
	// Staged is how many resource files the last successful apply wrote.
	Staged int `json:"staged"`
	// Expired reports that the dev run is gone and this pod is on its way out.
	Expired bool `json:"expired"`
}

// Coordinator owns the pull-and-apply cycle for one dev run.
type Coordinator struct {
	pull  puller
	apply applier
	now   func() time.Time

	mu      sync.Mutex
	running bool
	pending bool
	state   State

	expireOnce sync.Once
	expired    chan struct{}
}

// New returns a Coordinator pulling from p and writing through a.
func New(p puller, a applier) *Coordinator {
	return &Coordinator{
		pull:    p,
		apply:   a,
		now:     time.Now,
		expired: make(chan struct{}),
	}
}

// Expired is closed once the dev run is known to be gone — the pull returned
// {@link bundle.ErrGone} and the coordinator asked the orchestrator to expire the
// run. The process should shut down; a sidecar cannot delete its own workload,
// because it holds no Kubernetes credential by design.
func (c *Coordinator) Expired() <-chan struct{} { return c.expired }

// State returns a snapshot of what has happened so far.
func (c *Coordinator) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.state
	select {
	case <-c.expired:
		out.Expired = true
	default:
	}
	return out
}

// Reload pulls the bundle and writes it into the workspace.
//
// If a pull is already in flight the call coalesces into it and returns immediately
// with OutcomeCoalesced; the in-flight caller notices the pending flag and runs
// another round when it finishes. The caller driving those extra rounds reports
// their errors, which may belong to a later logical request than its own: success
// here means the workspace is current, not that this call is what made it so.
func (c *Coordinator) Reload(ctx context.Context) (Outcome, error) {
	c.mu.Lock()
	if c.running {
		c.pending = true
		c.mu.Unlock()
		return OutcomeCoalesced, nil
	}
	c.running = true
	c.mu.Unlock()

	// The first round serves this caller, under its context. Any further round serves
	// callers that coalesced in and have no context of their own, so it runs detached
	// from ctx (see onceDetached): binding it here would drop a promised reload the
	// moment this caller disconnects.
	err := c.once(ctx)
	for {
		c.mu.Lock()
		if !c.pending || err != nil {
			// Stop on error too: repeating a round that just failed would turn a
			// transient outage into a tight loop against it, and the next trigger is a
			// better clock than this one.
			c.pending = false
			c.running = false
			c.mu.Unlock()
			return OutcomeApplied, err
		}
		c.pending = false
		c.mu.Unlock()

		err = c.onceDetached(ctx)
	}
}

// onceDetached runs a round on behalf of coalesced callers, on a context detached
// from ctx's cancellation (its values are kept) and bounded by roundTimeout. This
// is what stops a disconnect by whoever triggered the in-flight pull from dropping
// a reload those callers were already promised.
func (c *Coordinator) onceDetached(ctx context.Context) error {
	roundCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), roundTimeout)
	defer cancel()
	return c.once(roundCtx)
}

// once performs a single pull and apply, recording the outcome.
func (c *Coordinator) once(ctx context.Context) error {
	b, err := c.pull.Fetch(ctx)
	if err != nil {
		c.record(err)
		if errors.Is(err, bundle.ErrGone) {
			c.expire(ctx)
		}
		return err
	}

	files := make([]workspace.File, 0, len(b.Resources))
	for _, r := range b.Resources {
		files = append(files, workspace.File{Name: r.Name, Content: r.Content})
	}

	report, err := c.apply.Apply(b.Definition, files)
	if err != nil {
		c.record(err)
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Generation = b.Generation
	c.state.LastPull = c.now()
	c.state.LastError = ""
	c.state.Pulls++
	c.state.Staged = report.Staged
	return nil
}

// record stores a failure for /status to report.
func (c *Coordinator) record(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.LastError = err.Error()
}

// expire tells the orchestrator to tear the dev run down and signals the process to
// stop. Best-effort by necessity: if the call fails there is nothing further this
// pod can do about its own existence.
func (c *Coordinator) expire(ctx context.Context) {
	c.expireOnce.Do(func() {
		if err := c.pull.Expire(ctx); err != nil {
			c.record(err)
		}
		close(c.expired)
	})
}
