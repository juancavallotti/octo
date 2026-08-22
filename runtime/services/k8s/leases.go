// The k8s module's fail-fast leases: one coordination Lease object per claimed
// name.
//
// It shares the API object with leader election next door and almost nothing
// else. An election campaigns — it keeps trying, and reports leadership when it
// eventually wins. A claim answers once: Create either succeeds, which is the
// claim, or conflicts, which is somebody else's. That is what lets a caller take
// a different path with the message it is holding instead of waiting on a run it
// cannot see.
package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	coordv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coordinationv1 "k8s.io/client-go/kubernetes/typed/coordination/v1"
)

// renewDivisor sets the renewal interval as a fraction of the lease TTL. Three
// renewals per TTL means two may be lost — to an API server blip, or a scheduler
// that stalled the goroutine — before the claim is at risk.
const renewDivisor = 3

// claimNamePrefix distinguishes a claim's object from leader election's, which
// uses octo-le-. They live in one namespace and are derived by the same hash, so
// a shared prefix would let an election and a claim on the same text collide on
// one object — and each would read the other's holder as its own competitor.
const claimNamePrefix = "octo-claim-"

// maxObjectName is the Kubernetes limit a derived name is truncated to.
const maxObjectName = 253

// leases claims names as coordination Lease objects in the deployment's namespace.
type leases struct {
	client       coordinationv1.CoordinationV1Interface
	namespace    string
	identity     string
	deploymentID string
	// now is the clock a holder's expiry is judged against, injected so a test can
	// age a claim without waiting for one.
	now func() time.Time
}

func newLeases(
	client coordinationv1.CoordinationV1Interface, namespace, identity, deploymentID string,
	now func() time.Time,
) *leases {
	return &leases{
		client: client, namespace: namespace, identity: identity,
		deploymentID: deploymentID, now: now,
	}
}

// Acquire claims name, or reports that somebody else holds it.
//
// Create is the claim: the API server admits exactly one, and every other
// caller gets AlreadyExists. That conflict is then read rather than trusted,
// because the object outlives its holder — a replica that died without releasing
// leaves one behind, and refusing forever on the strength of it would take the
// conversation out of service for good.
//
//nolint:ireturn // satisfies core.Leases
func (l *leases) Acquire(
	ctx context.Context, name string, opts ...core.LeaseOption,
) (core.Lease, bool, error) {
	cfg := core.NewLeaseConfig(opts...)
	object := claimName(l.deploymentID, name)

	created, err := l.client.Leases(l.namespace).Create(ctx, l.spec(object, cfg.TTL), metav1.CreateOptions{})
	switch {
	case err == nil:
		return l.hold(ctx, created, cfg.TTL), true, nil
	case apierrors.IsAlreadyExists(err):
		return l.takeOverExpired(ctx, object, cfg.TTL)
	default:
		return nil, false, fmt.Errorf("claim %s: create: %w", object, err)
	}
}

// takeOverExpired claims an object a previous holder left behind, and refuses one
// whose holder is still renewing.
//
// The read object's resourceVersion rides on the Update, so two challengers
// racing one dead holder cannot both win: the API server admits the first and
// gives the second a conflict, which is read here as "somebody else got there",
// not as an error.
//
//nolint:ireturn // satisfies core.Leases, whose Acquire returns core.Lease
func (l *leases) takeOverExpired(
	ctx context.Context, object string, ttl time.Duration,
) (core.Lease, bool, error) {
	current, err := l.client.Leases(l.namespace).Get(ctx, object, metav1.GetOptions{})
	if err != nil {
		// Gone between the conflict and the read: the holder released it. Nothing
		// is held now, but this call has already lost its race, and saying so is
		// better than a second round trip — the caller retries or moves on.
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("claim %s: read the current holder: %w", object, err)
	}
	if !l.expired(current) {
		return nil, false, nil
	}

	claimed := l.spec(object, ttl)
	claimed.ResourceVersion = current.ResourceVersion
	updated, err := l.client.Leases(l.namespace).Update(ctx, claimed, metav1.UpdateOptions{})
	if err != nil {
		if apierrors.IsConflict(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("claim %s: take over: %w", object, err)
	}
	slog.Debug("took over an expired claim",
		"object", object, "previous", holderOf(current), "identity", l.identity)
	return l.hold(ctx, updated, ttl), true, nil
}

// expired reports whether a claim's holder has stopped renewing. A record with no
// renewal time at all is treated as expired: it names no moment this could be
// measured from, and holding a name on the strength of it would be permanent.
func (l *leases) expired(record *coordv1.Lease) bool {
	if record.Spec.RenewTime == nil || record.Spec.LeaseDurationSeconds == nil {
		return true
	}
	deadline := record.Spec.RenewTime.Add(time.Duration(*record.Spec.LeaseDurationSeconds) * time.Second)
	return l.now().After(deadline)
}

// spec is the object this replica writes when it claims a name.
func (l *leases) spec(object string, ttl time.Duration) *coordv1.Lease {
	stamp := metav1.NewMicroTime(l.now())
	seconds := int32(ttl.Seconds())
	return &coordv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: object, Namespace: l.namespace},
		Spec: coordv1.LeaseSpec{
			HolderIdentity:       &l.identity,
			LeaseDurationSeconds: &seconds,
			AcquireTime:          &stamp,
			RenewTime:            &stamp,
		},
	}
}

// hold wraps a claimed object in a Lease handle and starts renewing it.
func (l *leases) hold(ctx context.Context, record *coordv1.Lease, ttl time.Duration) *heldLease {
	held := &heldLease{owner: l, object: record.Name, version: record.ResourceVersion, done: make(chan struct{})}
	// Renewal outlives the call that acquired the claim — an Acquire's context is
	// the request's, and the claim is the run's — so it runs on a context detached
	// from it and cancelled by Close instead.
	renewCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	held.cancel = cancel
	go held.renew(renewCtx, ttl)
	return held
}

// heldLease is one claimed name.
type heldLease struct {
	owner  *leases
	object string
	cancel context.CancelFunc
	done   chan struct{}

	mu sync.Mutex
	// version is the resourceVersion of the last write this replica made, so a
	// renewal cannot overwrite a successor that took the object over.
	version string
	lost    bool
}

// Done is closed when the claim is gone: released, or lost because a renewal did
// not land.
func (h *heldLease) Done() <-chan struct{} { return h.done }

// Close releases the claim by deleting its object, and is idempotent.
//
// The delete is conditional on the resourceVersion this replica last wrote, so a
// claim that was already taken over is not deleted out from under its successor.
// A failure to reach the API server is not returned: the claim is gone locally
// either way, and the object expires on its own.
func (h *heldLease) Close() error {
	h.mu.Lock()
	if h.lost {
		h.mu.Unlock()
		return nil
	}
	h.lost = true
	version := h.version
	h.mu.Unlock()

	h.cancel()
	close(h.done)

	ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
	defer cancel()
	err := h.owner.client.Leases(h.owner.namespace).Delete(ctx, h.object, metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{ResourceVersion: &version},
	})
	if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
		slog.Warn("could not delete a released claim; it will expire instead",
			"object", h.object, "error", err)
	}
	return nil
}

// releaseTimeout bounds the delete a release makes. The claim is already gone as
// far as this replica is concerned, so this only decides how long shutdown waits
// before leaving the object to expire.
const releaseTimeout = 5 * time.Second

// renew pushes the claim's deadline out for as long as it is held. Losing it is
// not an error to report anywhere: it is reported by closing Done, which is what
// the holder is gating its work on.
func (h *heldLease) renew(ctx context.Context, ttl time.Duration) {
	ticker := time.NewTicker(ttl / renewDivisor)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.done:
			return
		case <-ticker.C:
			if err := h.extend(ctx, ttl); err != nil {
				slog.Warn("lost a claim: its renewal did not land",
					"object", h.object, "identity", h.owner.identity, "error", err)
				h.lose()
				return
			}
		}
	}
}

// extend writes a fresh renewal time, refusing to do so once the object names
// somebody else as its holder — which is what a replica whose renewals were
// delayed past the TTL finds when it comes back.
func (h *heldLease) extend(ctx context.Context, ttl time.Duration) error {
	current, err := h.owner.client.Leases(h.owner.namespace).Get(ctx, h.object, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("renew %s: read: %w", h.object, err)
	}
	if holderOf(current) != h.owner.identity {
		return errClaimTakenOver
	}

	renewed := h.owner.spec(h.object, ttl)
	renewed.Spec.AcquireTime = current.Spec.AcquireTime
	renewed.ResourceVersion = current.ResourceVersion
	updated, err := h.owner.client.Leases(h.owner.namespace).Update(ctx, renewed, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("renew %s: %w", h.object, err)
	}

	h.mu.Lock()
	h.version = updated.ResourceVersion
	h.mu.Unlock()
	return nil
}

// lose marks the claim gone without touching the API server — the object now
// belongs to somebody else, or could not be reached.
func (h *heldLease) lose() {
	h.mu.Lock()
	if h.lost {
		h.mu.Unlock()
		return
	}
	h.lost = true
	h.mu.Unlock()

	h.cancel()
	close(h.done)
}

// errClaimTakenOver is a renewal that found the object naming a different holder.
var errClaimTakenOver = errors.New("the claim was taken over by another replica")

// holderOf reads a record's holder, tolerating one that names none.
func holderOf(record *coordv1.Lease) string {
	if record.Spec.HolderIdentity == nil {
		return ""
	}
	return *record.Spec.HolderIdentity
}

// claimName derives the object name for a claim: the claim prefix, the sanitized
// deployment id, and a short hash of the name (so arbitrary name text never
// produces an invalid object name).
func claimName(deploymentID, name string) string {
	sum := sha256.Sum256([]byte(name))
	object := claimNamePrefix + sanitizeDNS(deploymentID) + "-" + hex.EncodeToString(sum[:])[:10]
	if len(object) > maxObjectName {
		object = object[:maxObjectName]
	}
	return object
}
