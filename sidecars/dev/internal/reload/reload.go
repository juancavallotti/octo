// Package reload turns "go look again" into at most one pull at a time.
//
// A reload is triggered by a save, and saves arrive in bursts — the editor
// debounces edits at 2s, and one user hitting save repeatedly is the normal case
// rather than the pathological one. Handling each arrival independently would mean
// N concurrent fetches of the same bundle and N interleaved writers into the one
// directory the runtime is watching, which is how a workspace ends up holding
// half of two generations.
//
// So a pull is serialised, and a request arriving while one is in flight sets a
// pending flag instead of queueing behind it. That is the whole design, and the
// flag rather than a queue is the point: the second and the fiftieth request in a
// burst want exactly the same thing — the latest bundle — so collapsing them
// costs nothing and bounds the work at one extra round.
package reload

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/juancavallotti/octo/sidecars/dev/internal/bundle"
	"github.com/juancavallotti/octo/sidecars/dev/internal/workspace"
)

// puller is the orchestrator client this needs. Declared here, in the consumer, so
// a test drives a fake with no HTTP server; satisfied structurally by
// *bundle.Client.
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
	// repeat and returned. The reload WILL happen; it just is not this call's to
	// report on.
	OutcomeCoalesced
)

// State is a snapshot of what the coordinator has done, for GET /status.
type State struct {
	// Generation is the marker the orchestrator stamped on the last applied bundle.
	Generation string `json:"generation,omitempty"`
	// LastPull is when a pull last succeeded; zero if none has.
	LastPull time.Time `json:"lastPull,omitzero"`
	// LastError is the last pull or apply failure, cleared by the next success.
	// Retained rather than only logged: a reload that failed twenty minutes ago is
	// exactly what someone staring at a stale app needs to see.
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
// If a pull is already in flight the call coalesces into it and returns
// immediately with OutcomeCoalesced; the in-flight caller notices the pending flag
// and runs another round when it finishes. One consequence worth naming: the
// caller that drives the extra rounds reports THEIR error, which may belong to a
// later logical request than its own. That is the right trade for a reload — what
// matters is that the newest bundle lands, not which HTTP response carries the
// news — but it does mean a 200 here means "the workspace is current", not
// "your specific request was the one that made it so".
func (c *Coordinator) Reload(ctx context.Context) (Outcome, error) {
	c.mu.Lock()
	if c.running {
		c.pending = true
		c.mu.Unlock()
		return OutcomeCoalesced, nil
	}
	c.running = true
	c.mu.Unlock()

	var err error
	for {
		err = c.once(ctx)

		c.mu.Lock()
		if !c.pending || err != nil {
			// Stop on error too. Repeating a round that just failed would turn a
			// transient orchestrator outage into a tight loop against it; the next
			// save, or the caller's own retry, is a better clock than this one.
			c.pending = false
			c.running = false
			c.mu.Unlock()
			return OutcomeApplied, err
		}
		c.pending = false
		c.mu.Unlock()
	}
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

// expire tells the orchestrator to tear the dev run down and signals the process
// to stop. Best-effort by necessity: if the call fails there is nothing further
// this pod can do about its own existence, and the orchestrator's idle reaper is
// the backstop that always eventually collects it.
func (c *Coordinator) expire(ctx context.Context) {
	c.expireOnce.Do(func() {
		if err := c.pull.Expire(ctx); err != nil {
			c.record(err)
		}
		close(c.expired)
	})
}
