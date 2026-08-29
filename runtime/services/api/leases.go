// The api module's fail-fast leases: one claim per name, held on the platform
// API and kept alive by renewal.
//
// It shares its shape with leader election next door and almost nothing else. An
// election campaigns — it keeps asking, and reports leadership when it eventually
// wins. A claim answers once: acquired, or somebody else's. That is what lets a
// caller take a different path with the message it is holding instead of waiting
// on a run it cannot see.
package api

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// renewDivisor sets the renewal interval as a fraction of the lease TTL. Three
// renewals per TTL means two may be lost — to a platform blip, or a scheduler
// that stalled the goroutine — before the claim is at risk.
const renewDivisor = 3

// releaseTimeout bounds the release call. The claim is already gone as far as
// this replica is concerned, so this only decides how long shutdown waits before
// leaving it to expire.
const releaseTimeout = 5 * time.Second

// leases claims names on the platform API.
type leases struct {
	c *client
	// module ends when Services.Close is called, so a claim's renewals stop with
	// the module even though they are deliberately detached from the call that
	// acquired it.
	module context.Context //nolint:containedctx // the module's lifetime, not a request's
	latch  *latch
	holder string
	// minTTL and maxTTL are the platform's declared bounds, applied after
	// core.NewLeaseConfig so a caller's option is honoured where it can be.
	minTTL time.Duration
	maxTTL time.Duration
}

func newLeases(module context.Context, c *client, holder string, f leaseFeature) *leases {
	return &leases{
		c:      c,
		module: module,
		latch:  &latch{feature: FeatureLeases},
		holder: holder,
		minTTL: time.Duration(f.MinTTLSeconds) * time.Second,
		maxTTL: time.Duration(f.MaxTTLSeconds) * time.Second,
	}
}

// acquireRequest and acquireResponse are the acquire call's wire types.
//
// acquired is a field of a 200 rather than a status code of its own, because
// "somebody else holds it" is the expected, non-exceptional answer — core.Leases
// is explicit that it is a decision, not a failure — and modelling it as an error
// status invites an implementer to log it as one. A 409 is accepted too, so an
// implementer who reaches for it is not wrong.
type acquireRequest struct {
	Name       string `json:"name"`
	Holder     string `json:"holder"`
	TTLSeconds int64  `json:"ttlSeconds"`
}

type acquireResponse struct {
	Acquired bool   `json:"acquired"`
	LeaseID  string `json:"leaseId"`
	Holder   string `json:"holder"`
}

// renewRequest asks for the claim's deadline to be pushed out.
type renewRequest struct {
	TTLSeconds int64 `json:"ttlSeconds"`
}

// Acquire claims name, or reports that somebody else holds it.
//
//nolint:ireturn // satisfies core.Leases
func (l *leases) Acquire(
	ctx context.Context, name string, opts ...core.LeaseOption,
) (core.Lease, bool, error) {
	if !l.latch.live() {
		return nil, false, unsupportedError(FeatureLeases)
	}
	ttl := l.clampTTL(core.NewLeaseConfig(opts...).TTL)

	var out acquireResponse
	err := l.c.json(ctx, routeLeaseAcquire, l.c.url(routeLeaseAcquire), acquireRequest{
		Name: name, Holder: l.holder, TTLSeconds: seconds(ttl),
	}, &out, l.c.timeout)
	switch {
	case err == nil:
	// A conflict is the other way an implementer can say "taken". It is not an
	// error here for the same reason acquired:false is not.
	case isVersionConflict(err):
		return nil, false, nil
	case isNotImplemented(err):
		l.latch.mark()
		return nil, false, unsupportedError(FeatureLeases)
	default:
		return nil, false, err
	}
	if !out.Acquired {
		slog.Debug("api: a claim is held elsewhere", "name", name, "holder", out.Holder)
		return nil, false, nil
	}
	return l.hold(ctx, out.LeaseID, name, ttl), true, nil
}

// clampTTL applies the platform's declared bounds. core.NewLeaseConfig has
// already floored the value at core.MinLeaseTTL, so this only narrows further.
func (l *leases) clampTTL(ttl time.Duration) time.Duration {
	if l.minTTL > 0 && ttl < l.minTTL {
		return l.minTTL
	}
	if l.maxTTL > 0 && ttl > l.maxTTL {
		return l.maxTTL
	}
	return ttl
}

// hold wraps a granted claim and starts renewing it.
func (l *leases) hold(ctx context.Context, leaseID, name string, ttl time.Duration) *heldLease {
	held := &heldLease{owner: l, id: leaseID, name: name, done: make(chan struct{})}
	// Renewal outlives the call that acquired the claim — an Acquire's context is
	// the request's, and the claim is the run's — so it runs on a context detached
	// from it, cancelled by Close or by the module shutting down.
	renewCtx, cancel := bindTo(context.WithoutCancel(ctx), l.module)
	held.cancel = cancel
	go held.renew(renewCtx, ttl)
	return held
}

// heldLease is one claimed name.
type heldLease struct {
	owner  *leases
	id     string
	name   string
	cancel context.CancelFunc
	done   chan struct{}

	mu   sync.Mutex
	lost bool
}

// Done is closed when the claim is gone: released, or lost because a renewal did
// not land.
func (h *heldLease) Done() <-chan struct{} { return h.done }

// Close releases the claim, and is idempotent.
//
// A failure to reach the platform is not returned: the claim is gone locally
// either way, and it expires on its own.
func (h *heldLease) Close() error {
	if !h.markGone() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
	defer cancel()
	err := h.owner.c.json(ctx, routeLeaseRelease,
		h.owner.c.url(routeLeaseRelease, h.id), nil, nil, releaseTimeout)
	if err != nil {
		slog.Warn("api: could not release a claim; it will expire instead",
			"name", h.name, "lease", h.id, "error", err)
	}
	return nil
}

// markGone closes the claim locally, reporting whether this call was the one that
// did it. Every path that ends a claim goes through it, so Done is closed exactly
// once.
func (h *heldLease) markGone() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lost {
		return false
	}
	h.lost = true
	h.cancel()
	close(h.done)
	return true
}

// renew pushes the claim's deadline out for as long as it is held. Losing it is
// not an error to report anywhere: it is reported by closing Done, which is what
// the holder gates its work on.
//
// A failed renewal is not automatically a lost claim, and the distinction is the
// whole reason for renewing three times per TTL. A 404 or a 409 is definitive —
// the claim is somebody else's now, or gone — and gives up immediately. Anything
// else is a network or a server having a moment, and the claim is still ours
// until the TTL runs out without a renewal landing, so the loop keeps trying. A
// runtime that dropped its claim on one refused connection would hand the work
// to another replica over a blip, which is exactly what the renewal budget
// exists to prevent.
func (h *heldLease) renew(ctx context.Context, ttl time.Duration) {
	ticker := time.NewTicker(ttl / renewDivisor)
	defer ticker.Stop()
	lastLanded := time.Now()
	for {
		select {
		case <-ctx.Done():
			// The module is shutting down, or Close cancelled us. Either way the
			// claim stops being renewed, so it stops being held — and a holder
			// gating work on Done has to hear that rather than carry on against a
			// claim quietly expiring on the platform.
			h.markGone()
			return
		case <-h.done:
			return
		case <-ticker.C:
			err := h.extend(ctx, ttl)
			if err == nil {
				lastLanded = time.Now()
				continue
			}
			if h.giveUp(err, lastLanded, ttl) {
				return
			}
		}
	}
}

// giveUp decides what a failed renewal means, reporting whether the claim is
// over. It closes Done when it is.
func (h *heldLease) giveUp(err error, lastLanded time.Time, ttl time.Duration) bool {
	definitive := errors.Is(err, errAbsent) || isVersionConflict(err)
	if !definitive && time.Since(lastLanded) < ttl {
		slog.Warn("api: a claim renewal did not land; retrying while the claim is still ours",
			"name", h.name, "lease", h.id, "remaining", ttl-time.Since(lastLanded), "error", err)
		return false
	}
	slog.Warn("api: lost a claim",
		"name", h.name, "lease", h.id, "holder", h.owner.holder,
		"reason", lossReason(definitive), "error", err)
	h.markGone()
	return true
}

// lossReason names why a claim ended, so the log line says whether somebody took
// it or this replica simply could not keep up.
func lossReason(definitive bool) string {
	if definitive {
		return "the platform says the claim is no longer ours"
	}
	return "no renewal landed within the claim's TTL"
}

// extend writes a fresh deadline. A 404 or a 409 means the claim is no longer
// this replica's — expired and taken over, or released behind our back — and both
// are definitive, unlike a transport failure.
func (h *heldLease) extend(ctx context.Context, ttl time.Duration) error {
	return h.owner.c.json(ctx, routeLeaseRenew, h.owner.c.url(routeLeaseRenew, h.id),
		renewRequest{TTLSeconds: seconds(ttl)}, nil, h.owner.c.timeout)
}

// seconds rounds a duration UP to whole seconds.
//
// Up, because rounding down would ask for a deadline earlier than the one this
// replica renews against: a challenger reading the claim would find it expired
// while its owner still believed it held. core.NewLeaseConfig guarantees at least
// core.MinLeaseTTL, so this never rounds to zero. Capped because an absurd TTL
// should not wrap into a negative one — a claim that never expires.
func seconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	// Quotient and remainder rather than (d + time.Second - 1): the addition
	// overflows for a duration near the top of the int64 range and rounds UP into
	// a negative, which would put a negative ttlSeconds on the wire — a claim the
	// platform would read as already expired, or as never expiring.
	whole, rest := int64(d/time.Second), d%time.Second
	if rest > 0 {
		whole++
	}
	if whole > math.MaxInt32 {
		return math.MaxInt32
	}
	return whole
}
