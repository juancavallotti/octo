package ingest

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDeployments answers lookups from a fixed map, counting how often it is
// asked and optionally blocking so a concurrent burst can be held mid-flight.
type fakeDeployments struct {
	mu       sync.Mutex
	owners   map[string]string
	err      error
	calls    atomic.Int64
	entered  chan struct{}
	released chan struct{}
}

func (f *fakeDeployments) IntegrationOf(_ context.Context, deploymentID string) (string, bool, error) {
	f.calls.Add(1)
	if f.entered != nil {
		f.entered <- struct{}{}
		<-f.released
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", false, f.err
	}
	owner, ok := f.owners[deploymentID]
	return owner, ok, nil
}

func (f *fakeDeployments) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeDeployments) own(deploymentID, integrationID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners[deploymentID] = integrationID
}

func newFake(owners map[string]string) *fakeDeployments {
	if owners == nil {
		owners = map[string]string{}
	}
	return &fakeDeployments{owners: owners}
}

// A deployment never changes hands, so resolving it twice must not ask twice. At
// trace volumes the alternative is an indexed query in front of every block
// invocation of every message.
func TestResolveCachesAHitForever(t *testing.T) {
	lookup := newFake(map[string]string{"dep-1": "int-1"})
	resolver := NewIntegrationResolver(lookup)
	ctx := context.Background()

	for i := range 100 {
		got, found, err := resolver.Resolve(ctx, "dep-1")
		if err != nil || !found || got != "int-1" {
			t.Fatalf("call %d: got %q found=%v err=%v", i, got, found, err)
		}
	}
	if n := lookup.calls.Load(); n != 1 {
		t.Fatalf("asked the database %d times, want 1", n)
	}
}

// A resolved answer has no deadline: winding the clock forward must not make the
// resolver ask again.
func TestResolveNeverReExpiresAHit(t *testing.T) {
	lookup := newFake(map[string]string{"dep-1": "int-1"})
	resolver := NewIntegrationResolver(lookup)
	clock := time.Now()
	resolver.now = func() time.Time { return clock }
	ctx := context.Background()

	if _, _, err := resolver.Resolve(ctx, "dep-1"); err != nil {
		t.Fatalf("first: %v", err)
	}
	clock = clock.Add(365 * 24 * time.Hour)
	if _, _, err := resolver.Resolve(ctx, "dep-1"); err != nil {
		t.Fatalf("second: %v", err)
	}

	if n := lookup.calls.Load(); n != 1 {
		t.Fatalf("asked the database %d times, want 1", n)
	}
}

// Records outlive the deployments that produced them, so an unknown id is
// expected rather than exceptional — and a burst of them must not become a burst
// of queries.
func TestResolveCachesAMissBriefly(t *testing.T) {
	lookup := newFake(nil)
	resolver := NewIntegrationResolver(lookup)
	clock := time.Now()
	resolver.now = func() time.Time { return clock }
	ctx := context.Background()

	for range 50 {
		_, found, err := resolver.Resolve(ctx, "dep-gone")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if found {
			t.Fatal("an unknown deployment resolved to an owner")
		}
	}
	if n := lookup.calls.Load(); n != 1 {
		t.Fatalf("asked the database %d times during the burst, want 1", n)
	}

	// A deployment created moments ago must start resolving shortly after, not
	// at the next restart.
	clock = clock.Add(unknownTTL + time.Second)
	lookup.own("dep-gone", "int-new")

	got, found, err := resolver.Resolve(ctx, "dep-gone")
	if err != nil || !found || got != "int-new" {
		t.Fatalf("after the negative memory lapsed: got %q found=%v err=%v", got, found, err)
	}
	if n := lookup.calls.Load(); n != 2 {
		t.Fatalf("asked the database %d times, want 2", n)
	}
}

// A failure says nothing about the deployment. Remembering it would keep
// answering with a database outage after the database came back.
func TestResolveDoesNotCacheAFailure(t *testing.T) {
	lookup := newFake(map[string]string{"dep-1": "int-1"})
	lookup.fail(errors.New("database unavailable"))
	resolver := NewIntegrationResolver(lookup)
	ctx := context.Background()

	if _, _, err := resolver.Resolve(ctx, "dep-1"); err == nil {
		t.Fatal("a failed lookup reported success")
	}

	lookup.fail(nil)
	got, found, err := resolver.Resolve(ctx, "dep-1")
	if err != nil || !found || got != "int-1" {
		t.Fatalf("after recovery: got %q found=%v err=%v", got, found, err)
	}
	if n := lookup.calls.Load(); n != 2 {
		t.Fatalf("asked the database %d times, want 2 — the failure was cached", n)
	}
}

// A cold start under load asks about the same deployment from every worker at
// once. They must share one query rather than issue one each.
func TestResolveSharesOneQueryAcrossConcurrentCallers(t *testing.T) {
	const callers = 32
	lookup := newFake(map[string]string{"dep-1": "int-1"})
	lookup.entered = make(chan struct{})
	lookup.released = make(chan struct{})
	resolver := NewIntegrationResolver(lookup)

	results := make(chan string, callers)
	var started sync.WaitGroup
	started.Add(callers)
	for range callers {
		go func() {
			started.Done()
			got, _, err := resolver.Resolve(context.Background(), "dep-1")
			if err != nil {
				results <- "error: " + err.Error()
				return
			}
			results <- got
		}()
	}
	started.Wait()

	// Hold the one lookup that got through, so every other caller is parked on
	// it rather than racing to start its own.
	<-lookup.entered
	close(lookup.released)

	for i := range callers {
		if got := <-results; got != "int-1" {
			t.Fatalf("caller %d got %q, want int-1", i, got)
		}
	}
	if n := lookup.calls.Load(); n != 1 {
		t.Fatalf("asked the database %d times, want 1", n)
	}
}

// The cache is a bound, not a leak. Overflowing it must keep answering
// correctly, whatever it costs in re-lookups.
func TestResolveSurvivesOverflowingItsCapacity(t *testing.T) {
	lookup := newFake(map[string]string{"dep-keeper": "int-keeper"})
	resolver := NewIntegrationResolver(lookup)
	ctx := context.Background()

	if _, _, err := resolver.Resolve(ctx, "dep-keeper"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for i := range resolverCapacity + 10 {
		if _, _, err := resolver.Resolve(ctx, "dep-"+strconv.Itoa(i)+"-flood"); err != nil {
			t.Fatalf("flood %d: %v", i, err)
		}
	}

	got, found, err := resolver.Resolve(ctx, "dep-keeper")
	if err != nil || !found || got != "int-keeper" {
		t.Fatalf("after overflow: got %q found=%v err=%v", got, found, err)
	}

	resolver.mu.Lock()
	held := len(resolver.entries)
	resolver.mu.Unlock()
	if held > resolverCapacity {
		t.Fatalf("cache holds %d entries, want at most %d", held, resolverCapacity)
	}
	if held >= resolverCapacity+10 {
		t.Fatalf("cache holds %d entries; the capacity clear never fired", held)
	}
}

// A caller giving up must not be left parked on someone else's query forever.
func TestResolveHonoursContextCancellationWhileWaiting(t *testing.T) {
	lookup := newFake(map[string]string{"dep-1": "int-1"})
	lookup.entered = make(chan struct{})
	lookup.released = make(chan struct{})
	resolver := NewIntegrationResolver(lookup)

	go func() { _, _, _ = resolver.Resolve(context.Background(), "dep-1") }()
	<-lookup.entered
	defer close(lookup.released)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := resolver.Resolve(ctx, "dep-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// TestResolverSharedLookupSurvivesTheFirstCallerLeaving covers the hazard in
// sharing one query: everyone waiting takes its answer, so running it under the
// first caller's context would let that caller's cancellation resolve the
// deployment as unknown for every record behind it.
func TestResolverSharedLookupSurvivesTheFirstCallerLeaving(t *testing.T) {
	release := make(chan struct{})
	lookup := &blockingLookup{started: make(chan struct{}), release: release, integrationID: "int-1"}
	resolver := NewIntegrationResolver(lookup)

	// The owner's context is cancelled while its query is still running.
	ownerCtx, cancelOwner := context.WithCancel(context.Background())
	ownerDone := make(chan struct{})
	go func() {
		defer close(ownerDone)
		_, _, _ = resolver.Resolve(ownerCtx, "dep-1")
	}()

	<-lookup.started
	cancelOwner()
	close(release)
	<-ownerDone

	// A caller behind it must still get the real answer, not the owner's regret.
	id, found, err := resolver.Resolve(context.Background(), "dep-1")
	if err != nil {
		t.Fatalf("resolve after the owner left: %v", err)
	}
	if !found || id != "int-1" {
		t.Errorf("resolved %q/%v, want int-1/true", id, found)
	}
	if got := lookup.calls.Load(); got != 1 {
		t.Errorf("queried %d times, want 1 — the answer was already in hand", got)
	}
}

// blockingLookup holds its answer until released, and reports the context it was
// given so a test can tell whether it inherited a cancellation.
type blockingLookup struct {
	started       chan struct{}
	release       chan struct{}
	integrationID string
	calls         atomic.Int64
	startOnce     sync.Once
}

func (b *blockingLookup) IntegrationOf(ctx context.Context, _ string) (string, bool, error) {
	b.calls.Add(1)
	b.startOnce.Do(func() { close(b.started) })
	select {
	case <-b.release:
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
	return b.integrationID, true, nil
}
