package reload

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/sidecars/dev/internal/bundle"
	"github.com/juancavallotti/octo/sidecars/dev/internal/workspace"
)

// fakePuller is a hand-written stand-in for the orchestrator client. `release`, when
// non-nil, blocks Fetch until the test closes it — which is what makes the
// coalescing behaviour observable without any timing assumptions.
type fakePuller struct {
	mu        sync.Mutex
	fetches   int
	expires   int
	bundles   []bundle.Bundle
	errs      []error
	release   chan struct{}
	entered   chan struct{}
	expireErr error
}

func (f *fakePuller) Fetch(context.Context) (bundle.Bundle, error) {
	f.mu.Lock()
	n := f.fetches
	f.fetches++
	f.mu.Unlock()

	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.release != nil {
		<-f.release
	}

	var err error
	if n < len(f.errs) {
		err = f.errs[n]
	}
	var b bundle.Bundle
	if n < len(f.bundles) {
		b = f.bundles[n]
	}
	return b, err
}

func (f *fakePuller) Expire(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expires++
	return f.expireErr
}

func (f *fakePuller) counts() (fetches, expires int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetches, f.expires
}

// fakeApplier records what was written, and can be made to fail.
type fakeApplier struct {
	mu      sync.Mutex
	applies int
	last    string
	files   []workspace.File
	err     error
}

func (f *fakeApplier) Apply(definition string, files []workspace.File) (workspace.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applies++
	f.last = definition
	f.files = files
	if f.err != nil {
		return workspace.Report{}, f.err
	}
	return workspace.Report{Staged: len(files)}, nil
}

func (f *fakeApplier) snapshot() (applies int, last string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.applies, f.last
}

func TestReloadPullsAndApplies(t *testing.T) {
	p := &fakePuller{bundles: []bundle.Bundle{{
		Definition: "name: demo\n",
		Generation: "g1",
		Resources:  []bundle.Resource{{Name: ".env.dev", Content: "A=1\n"}},
	}}}
	a := &fakeApplier{}
	c := New(p, a)

	outcome, err := c.Reload(context.Background())
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if outcome != OutcomeApplied {
		t.Errorf("outcome = %v, want OutcomeApplied", outcome)
	}

	if applies, last := a.snapshot(); applies != 1 || last != "name: demo\n" {
		t.Errorf("applies = %d, definition = %q", applies, last)
	}
	if len(a.files) != 1 || a.files[0].Name != ".env.dev" {
		t.Errorf("files = %+v", a.files)
	}

	st := c.State()
	if st.Pulls != 1 || st.Generation != "g1" || st.Staged != 1 {
		t.Errorf("state = %+v", st)
	}
	if st.LastError != "" {
		t.Errorf("LastError = %q, want empty", st.LastError)
	}
	if st.LastPull.IsZero() {
		t.Error("LastPull is zero, want a timestamp")
	}
}

// The burst case: a save storm must not become N concurrent fetches. A second
// request arriving mid-pull collapses into the in-flight one, which then runs
// exactly one more round to pick up whatever those requests were about.
func TestReloadCoalescesConcurrentRequests(t *testing.T) {
	p := &fakePuller{
		release: make(chan struct{}),
		entered: make(chan struct{}, 8),
		bundles: []bundle.Bundle{{Definition: "v1\n"}, {Definition: "v2\n"}},
	}
	a := &fakeApplier{}
	c := New(p, a)

	// Start the first reload and wait until it is genuinely inside Fetch.
	done := make(chan error, 1)
	go func() {
		_, err := c.Reload(context.Background())
		done <- err
	}()
	<-p.entered

	// Three more requests while the first is blocked: every one of them should
	// collapse rather than start its own pull.
	for i := range 3 {
		outcome, err := c.Reload(context.Background())
		if err != nil {
			t.Fatalf("collapsed Reload %d: %v", i, err)
		}
		if outcome != OutcomeCoalesced {
			t.Fatalf("outcome %d = %v, want OutcomeCoalesced", i, outcome)
		}
	}

	if fetches, _ := p.counts(); fetches != 1 {
		t.Fatalf("fetches while blocked = %d, want 1", fetches)
	}

	// Let the first pull finish; it should notice the pending flag and run one more
	// round — one, not three.
	close(p.release)
	if err := <-done; err != nil {
		t.Fatalf("first Reload: %v", err)
	}
	<-p.entered // the pending round

	if fetches, _ := p.counts(); fetches != 2 {
		t.Errorf("fetches = %d, want 2 (the original plus one pending round)", fetches)
	}
	if applies, last := a.snapshot(); applies != 2 || last != "v2\n" {
		t.Errorf("applies = %d, last = %q, want 2 and the newest bundle", applies, last)
	}
}

// ctxAwarePuller records the context each Fetch is given and, once released, fails
// if that context has been cancelled — modelling a pull whose work is abandoned
// when the request behind it goes away. `entered` carries each call's context so a
// test can rendezvous on entry; `release` gates each call one at a time.
type ctxAwarePuller struct {
	mu      sync.Mutex
	fetches int
	entered chan context.Context
	release chan struct{}
}

func (p *ctxAwarePuller) Fetch(ctx context.Context) (bundle.Bundle, error) {
	p.mu.Lock()
	p.fetches++
	n := p.fetches
	p.mu.Unlock()

	p.entered <- ctx
	<-p.release
	if err := ctx.Err(); err != nil {
		return bundle.Bundle{}, err
	}
	return bundle.Bundle{Definition: fmt.Sprintf("v%d\n", n)}, nil
}

func (p *ctxAwarePuller) Expire(context.Context) error { return nil }

func (p *ctxAwarePuller) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fetches
}

// A reload triggered by a caller who then disconnects must still run the round it
// promised the callers that coalesced into it — they already received
// OutcomeCoalesced. Binding the pending round to the triggering request's context
// would drop their reload the instant that caller's connection closed, leaving the
// workspace stale until the next save.
func TestReloadPendingRoundSurvivesCallerCancel(t *testing.T) {
	p := &ctxAwarePuller{
		entered: make(chan context.Context, 2),
		release: make(chan struct{}),
	}
	a := &fakeApplier{}
	c := New(p, a)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := c.Reload(ctx)
		done <- err
	}()
	<-p.entered // round 1 is inside Fetch

	// A second request coalesces into the in-flight pull.
	if outcome, err := c.Reload(context.Background()); err != nil || outcome != OutcomeCoalesced {
		t.Fatalf("coalesced Reload: outcome=%v, err=%v", outcome, err)
	}

	// Round 1 completes while its caller is still connected.
	p.release <- struct{}{}

	// The pending round has now started; only now does the triggering caller
	// disconnect. Its round must run to completion regardless.
	<-p.entered
	cancel()
	p.release <- struct{}{}

	if err := <-done; err != nil {
		t.Fatalf("Reload dropped the pending round after the caller disconnected: %v", err)
	}
	if got := p.count(); got != 2 {
		t.Errorf("fetches = %d, want 2 (the original plus the pending round)", got)
	}
	if applies, last := a.snapshot(); applies != 2 || last != "v2\n" {
		t.Errorf("applies = %d, last = %q, want 2 and the pending bundle", applies, last)
	}
}

// A failed round must not be retried in-loop: repeating it would turn a transient
// orchestrator outage into a tight loop against it.
func TestReloadStopsOnError(t *testing.T) {
	p := &fakePuller{
		release: make(chan struct{}),
		entered: make(chan struct{}, 4),
		errs:    []error{errors.New("orchestrator unreachable")},
	}
	c := New(p, &fakeApplier{})

	done := make(chan error, 1)
	go func() {
		_, err := c.Reload(context.Background())
		done <- err
	}()
	<-p.entered

	if _, err := c.Reload(context.Background()); err != nil {
		t.Fatalf("collapsed Reload: %v", err)
	}
	close(p.release)

	if err := <-done; err == nil {
		t.Fatal("Reload: want the pull error")
	}
	if fetches, _ := p.counts(); fetches != 1 {
		t.Errorf("fetches = %d, want 1 — a failed round must not drive the pending one", fetches)
	}
	if st := c.State(); st.LastError == "" {
		t.Error("LastError is empty, want the failure recorded for /status")
	}
}

// The terminal case: 404 means no retry can ever succeed, so the coordinator asks
// the orchestrator to tear the run down and signals the process to exit.
func TestReloadExpiresOnGone(t *testing.T) {
	p := &fakePuller{errs: []error{bundle.ErrGone}}
	c := New(p, &fakeApplier{})

	if _, err := c.Reload(context.Background()); !errors.Is(err, bundle.ErrGone) {
		t.Fatalf("err = %v, want ErrGone", err)
	}

	select {
	case <-c.Expired():
	case <-time.After(time.Second):
		t.Fatal("Expired() was not closed")
	}
	if _, expires := p.counts(); expires != 1 {
		t.Errorf("expires = %d, want 1", expires)
	}
	if st := c.State(); !st.Expired {
		t.Error("State().Expired = false, want true")
	}
}

// Expire is called once however many times the pull reports gone: a second call
// would be pointless, and closing an already-closed channel would panic.
func TestExpireHappensOnce(t *testing.T) {
	p := &fakePuller{errs: []error{bundle.ErrGone, bundle.ErrGone}}
	c := New(p, &fakeApplier{})

	for range 2 {
		if _, err := c.Reload(context.Background()); !errors.Is(err, bundle.ErrGone) {
			t.Fatalf("err = %v, want ErrGone", err)
		}
	}
	if _, expires := p.counts(); expires != 1 {
		t.Errorf("expires = %d, want 1", expires)
	}
}

// A rejected token is NOT terminal. A pod that deleted itself over a credential
// problem would destroy the evidence of the misconfiguration that caused it.
func TestUnauthorizedDoesNotExpire(t *testing.T) {
	p := &fakePuller{errs: []error{bundle.ErrUnauthorized}}
	c := New(p, &fakeApplier{})

	if _, err := c.Reload(context.Background()); !errors.Is(err, bundle.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	select {
	case <-c.Expired():
		t.Fatal("Expired() was closed on an auth failure")
	default:
	}
	if _, expires := p.counts(); expires != 0 {
		t.Errorf("expires = %d, want 0", expires)
	}
}

// An apply failure is recorded and reported, and does not count as a pull: /status
// must not claim a generation landed when it did not.
func TestApplyFailureIsReported(t *testing.T) {
	p := &fakePuller{bundles: []bundle.Bundle{{Definition: "v1\n", Generation: "g1"}}}
	c := New(p, &fakeApplier{err: errors.New("disk full")})

	if _, err := c.Reload(context.Background()); err == nil {
		t.Fatal("Reload: want the apply error")
	}
	st := c.State()
	if st.Pulls != 0 {
		t.Errorf("Pulls = %d, want 0", st.Pulls)
	}
	if st.Generation != "" {
		t.Errorf("Generation = %q, want empty", st.Generation)
	}
	if st.LastError == "" {
		t.Error("LastError is empty, want the apply failure")
	}
}

// A success clears the previous failure, so /status does not keep reporting a
// problem that has since been resolved.
func TestSuccessClearsLastError(t *testing.T) {
	p := &fakePuller{
		errs:    []error{errors.New("transient")},
		bundles: []bundle.Bundle{{}, {Definition: "v2\n", Generation: "g2"}},
	}
	c := New(p, &fakeApplier{})

	if _, err := c.Reload(context.Background()); err == nil {
		t.Fatal("first Reload: want an error")
	}
	if _, err := c.Reload(context.Background()); err != nil {
		t.Fatalf("second Reload: %v", err)
	}
	if st := c.State(); st.LastError != "" {
		t.Errorf("LastError = %q, want cleared by the success", st.LastError)
	}
}

// The coordinator is driven by concurrent HTTP handlers, so it has to be safe under
// -race with no external synchronisation.
func TestReloadIsRaceFree(t *testing.T) {
	p := &fakePuller{}
	c := New(p, &fakeApplier{})

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Reload(context.Background()); err != nil {
				t.Errorf("Reload: %v", err)
			}
			_ = c.State()
		}()
	}
	wg.Wait()

	if fetches, _ := p.counts(); fetches == 0 {
		t.Error("fetches = 0, want at least one")
	}
}
