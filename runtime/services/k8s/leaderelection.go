package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/juancavallotti/octo/core"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coordinationv1 "k8s.io/client-go/kubernetes/typed/coordination/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// Lease timings: a replica renews within renewDeadline of a leaseDuration window,
// and a challenger retries every retryPeriod. These are the conventional client-go
// defaults — short enough for prompt failover, long enough to tolerate brief API
// blips.
const (
	leaseDuration = 15 * time.Second
	renewDeadline = 10 * time.Second
	retryPeriod   = 2 * time.Second
)

// leaderElection acquires per-key Leases in the pod's namespace. Each key maps to a
// Lease named from the deployment id and a hash of the key, so keys are isolated
// per deployment and the name is always a valid object name.
type leaderElection struct {
	client       coordinationv1.CoordinationV1Interface
	namespace    string
	identity     string // this replica's identity (the pod name)
	deploymentID string
}

func newLeaderElection(
	client coordinationv1.CoordinationV1Interface, namespace, identity, deploymentID string,
) *leaderElection {
	return &leaderElection{client: client, namespace: namespace, identity: identity, deploymentID: deploymentID}
}

// Acquire starts campaigning for key in the background and returns a handle whose
// IsLeader tracks the current status. The campaign runs until the handle is closed
// or ctx is cancelled; losing leadership triggers a re-campaign so a replica can
// regain it later.
//
//nolint:ireturn // satisfies core.LeaderElection
func (le *leaderElection) Acquire(ctx context.Context, key string) (core.Leadership, error) {
	runCtx, cancel := context.WithCancel(ctx)
	lease := leaseName(le.deploymentID, key)
	l := &leadership{key: key, lease: lease, cancel: cancel, done: make(chan struct{})}

	slog.Debug("starting leader election campaign",
		"key", key, "lease", lease, "namespace", le.namespace, "identity", le.identity)

	lock := &resourcelock.LeaseLock{
		LeaseMeta:  metav1.ObjectMeta{Name: lease, Namespace: le.namespace},
		Client:     le.client,
		LockConfig: resourcelock.ResourceLockConfig{Identity: le.identity},
	}
	cfg := leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   leaseDuration,
		RenewDeadline:   renewDeadline,
		RetryPeriod:     retryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(context.Context) {
				l.leader.Store(true)
				slog.Info("acquired leadership", "key", key, "lease", lease, "identity", le.identity)
			},
			OnStoppedLeading: func() {
				l.leader.Store(false)
				slog.Info("lost leadership", "key", key, "lease", lease, "identity", le.identity)
			},
			OnNewLeader: func(identity string) {
				slog.Debug("observed leader for key", "key", key, "lease", lease, "leader", identity)
			},
		},
	}

	go le.campaign(runCtx, key, lease, cfg, l)
	return l, nil
}

// campaign runs the election loop until the context is cancelled. client-go's
// LeaderElector.Run returns when leadership is lost (or never won within a cycle);
// re-running keeps this replica a standby that can take over later.
func (le *leaderElection) campaign(
	ctx context.Context, key, lease string, cfg leaderelection.LeaderElectionConfig, l *leadership,
) {
	defer close(l.done)
	for ctx.Err() == nil {
		elector, err := leaderelection.NewLeaderElector(cfg)
		if err != nil {
			// Misconfigured timings are a programming error, not a runtime condition;
			// log and stop campaigning for this key rather than spin.
			slog.Error("leader election setup failed", "key", key, "lease", lease, "error", err)
			return
		}
		slog.Debug("campaigning for leadership", "key", key, "lease", lease, "identity", le.identity)
		elector.Run(ctx) // blocks until leadership is lost or ctx is cancelled
		l.leader.Store(false)
		if ctx.Err() == nil {
			slog.Debug("leadership cycle ended, re-campaigning", "key", key, "lease", lease)
		}
		select {
		case <-ctx.Done():
		case <-time.After(retryPeriod):
		}
	}
	slog.Debug("leader election campaign stopped", "key", key, "lease", lease)
}

// leadership is a handle to one key's campaign.
type leadership struct {
	key    string
	lease  string
	leader atomic.Bool
	cancel context.CancelFunc
	done   chan struct{}
}

func (l *leadership) IsLeader() bool { return l.leader.Load() }

// Close stops the campaign and waits for it to wind down (releasing the Lease when
// ReleaseOnCancel applies), so no further IsLeader transitions happen after it
// returns.
func (l *leadership) Close() error {
	slog.Debug("stopping leader election campaign", "key", l.key, "lease", l.lease)
	l.cancel()
	<-l.done
	return nil
}

// leaseName builds a DNS-1123 object name for a deployment+key pair: a stable
// prefix, the sanitized deployment id, and a short hash of the key (so arbitrary
// key text never produces an invalid name). The result is bounded well under the
// 253-char limit.
func leaseName(deploymentID, key string) string {
	sum := sha256.Sum256([]byte(key))
	short := hex.EncodeToString(sum[:])[:10]
	name := "octo-le-" + sanitizeDNS(deploymentID) + "-" + short
	const maxLen = 253
	if len(name) > maxLen {
		name = name[:maxLen]
	}
	return name
}

// sanitizeDNS lowercases s and replaces every character that is not a lowercase
// alphanumeric with '-', so it is safe inside a DNS-1123 name.
func sanitizeDNS(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}
