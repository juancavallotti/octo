// Package leader answers one question for the alerting runner: is this replica
// the one that should act right now.
//
// The observability service is a competing consumer everywhere else — two
// replicas share a NATS queue group and both write to the same tables, and that
// is correct because ingesting a record twice is idempotent. Evaluating a watch
// twice is not: two replicas would open two incidents, send two emails and race
// each other's state updates. So exactly one replica evaluates, and this is how
// it finds out that it is the one.
//
// It is a Kubernetes Lease, taken through client-go's leader election, with the
// same timings the runtime's own election uses. Off-cluster there is no API
// server to ask and no second replica to compete with, so the elector reports
// itself the leader — which is not a degraded stand-in but the complete and exact
// answer in a single process, the same relationship an in-process queue has to a
// NATS one.
package leader

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const (
	// The conventional client-go timings, matched to the runtime's own election
	// in runtime/services/k8s/leaderelection.go. Short enough for prompt
	// failover, long enough to tolerate a brief API-server blip — and identical
	// on both sides so an operator reading one has read the other.
	leaseDuration = 15 * time.Second
	renewDeadline = 10 * time.Second
	retryPeriod   = 2 * time.Second

	// LeaseName is the object this service campaigns for. One lease for the whole
	// service rather than one per watch: the work is a single tick that evaluates
	// everything due, so there is nothing to spread and a lease per watch would
	// be a lease per row.
	LeaseName = "octo-observability-alerting"

	// The downward-API variables the chart supplies. Both are required in-cluster
	// — an election needs a namespace to hold the lease and an identity to hold
	// it as — and their absence is exactly how this recognises that it is not in
	// a cluster at all.
	podNameVar      = "POD_NAME"
	podNamespaceVar = "POD_NAMESPACE"
)

// Elector reports whether this process currently holds the lease.
//
// The alerting runner asks once per tick and never blocks on the answer. A
// replica that is not the leader idles; it does not wait to become one, because
// the tick it would be waiting for has already been served by the replica that
// is.
type Elector struct {
	leader   atomic.Bool
	standby  bool // true when there is no election to run and this process simply acts
	identity string
}

// IsLeader reports whether this replica should act.
func (e *Elector) IsLeader() bool { return e.standby || e.leader.Load() }

// Identity is who this replica campaigns as, for logging.
func (e *Elector) Identity() string { return e.identity }

// New builds an elector for this process.
//
// Off-cluster — no in-cluster config, or no downward-API variables — it returns a
// standby elector that always acts, and says so once at startup. That is what
// makes `task observability:run` and the tests work, and it is honest: a single
// process competing with nobody is the leader by definition.
func New(ctx context.Context) (*Elector, error) {
	name, namespace := os.Getenv(podNameVar), os.Getenv(podNamespaceVar)
	if name == "" || namespace == "" {
		slog.Info("no pod identity in the environment; alerting will evaluate in this process",
			"reason", "POD_NAME or POD_NAMESPACE is unset")
		return &Elector{standby: true, identity: "standalone"}, nil
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		// Named rather than swallowed: a pod that has the downward-API variables
		// and cannot reach the API server is misconfigured, and a service that
		// silently elected itself would put two evaluators on one installation.
		return nil, fmt.Errorf("leader: in-cluster config for %s/%s: %w", namespace, name, err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("leader: build a kubernetes client: %w", err)
	}

	e := &Elector{identity: name}
	go e.campaign(ctx, client, namespace)
	return e, nil
}

// campaign runs the election until ctx is cancelled.
//
// client-go's LeaderElector.Run returns when leadership is lost or was never won
// within a cycle, so re-running is what keeps this replica a standby that can
// take over later rather than one that gave up the first time it lost.
func (e *Elector) campaign(ctx context.Context, client kubernetes.Interface, namespace string) {
	cfg := leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{Name: LeaseName, Namespace: namespace},
			Client:    client.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: e.identity,
			},
		},
		// Released on shutdown, so a rolling restart hands over in a couple of
		// seconds instead of leaving the installation unwatched for a whole lease
		// duration while the lease times out.
		ReleaseOnCancel: true,
		LeaseDuration:   leaseDuration,
		RenewDeadline:   renewDeadline,
		RetryPeriod:     retryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(context.Context) {
				e.leader.Store(true)
				slog.Info("this replica now evaluates alerting watches", "identity", e.identity)
			},
			OnStoppedLeading: func() {
				e.leader.Store(false)
				slog.Info("this replica no longer evaluates alerting watches", "identity", e.identity)
			},
			OnNewLeader: func(identity string) {
				slog.Debug("observed the alerting leader", "leader", identity)
			},
		},
	}

	for ctx.Err() == nil {
		elector, err := leaderelection.NewLeaderElector(cfg)
		if err != nil {
			// Misconfigured timings are a programming error rather than a runtime
			// condition. Stop campaigning rather than spin — and leave this
			// replica reporting that it is not the leader, which is true.
			slog.Error("alerting leader election could not start", "error", err)
			return
		}
		elector.Run(ctx) // blocks until leadership is lost or ctx is cancelled
		e.leader.Store(false)
		select {
		case <-ctx.Done():
		case <-time.After(retryPeriod):
		}
	}
	slog.Debug("alerting leader election stopped")
}
