package api

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// Campaign timings, deliberately the client-go numbers the k8s module already
// runs on, so failover timing does not silently change with the module a
// deployment picks.
//
// The relationship between them is the load-bearing part: a leader stops
// asserting leadership one renew interval after it loses contact, so the TTL must
// exceed several renew intervals for that to happen before anybody else's view of
// the claim expires. renewInterval is clamped to at most a third of the TTL for
// exactly that reason — a platform asking to be renewed every fourteen seconds of
// a fifteen-second TTL is asking for a split brain.
const (
	defaultLeaderTTL       = 15 * time.Second
	defaultRenewInterval   = 5 * time.Second
	defaultObserveInterval = 2 * time.Second
	renewIntervalDivisor   = 3
	resignTimeout          = 5 * time.Second
)

// leaderElection campaigns for keys on the platform API.
//
// core.Leadership is a long-lived observation and HTTP has no long-lived
// anything, so this polls. That is less of a compromise than it sounds:
// client-go's leader election is itself a poll loop over a Lease object, and this
// runs the same loop over its own endpoint.
type leaderElection struct {
	c      *client
	latch  *latch
	holder string

	ttl     time.Duration
	renew   time.Duration
	observe time.Duration
}

func newLeaderElection(c *client, holder string, f leaderFeature) *leaderElection {
	le := &leaderElection{
		c:       c,
		latch:   &latch{feature: FeatureLeaderElection},
		holder:  holder,
		ttl:     orDefault(f.LeaseTTLSeconds, defaultLeaderTTL),
		renew:   orDefault(f.RenewIntervalSeconds, defaultRenewInterval),
		observe: orDefault(f.ObserveIntervalSeconds, defaultObserveInterval),
	}
	if maxRenew := le.ttl / renewIntervalDivisor; le.renew > maxRenew {
		slog.Warn("api: the platform API asked for a renew interval too close to the lease TTL; "+
			"shortening it, because a leader that cannot renew in time is a split brain",
			"declared", le.renew, "using", maxRenew, "ttl", le.ttl)
		le.renew = maxRenew
	}
	return le
}

// orDefault reads a declared duration in seconds, falling back when unset.
func orDefault(seconds int, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

// campaignRequest claims a key. It is sent for the first claim and for every
// renewal alike, because to a stateless server they are the same question.
type campaignRequest struct {
	Holder     string `json:"holder"`
	TTLSeconds int64  `json:"ttlSeconds"`
}

type campaignResponse struct {
	Leader        bool   `json:"leader"`
	LeaseID       string `json:"leaseId"`
	CurrentLeader string `json:"currentLeader"`
}

// resignRequest gives a key up.
type resignRequest struct {
	LeaseID string `json:"leaseId"`
}

// Acquire starts campaigning for key in the background and returns a handle whose
// IsLeader tracks the current status.
//
// It returns immediately with IsLeader false, which is what core.LeaderElection
// promises: a false reading means "not yet", not "somebody else has it".
//
//nolint:ireturn // satisfies core.LeaderElection
func (le *leaderElection) Acquire(ctx context.Context, key string) (core.Leadership, error) {
	if !le.latch.live() {
		return nil, unsupportedError(FeatureLeaderElection)
	}
	runCtx, cancel := context.WithCancel(ctx)
	l := &leadership{owner: le, key: key, cancel: cancel, done: make(chan struct{})}
	slog.Debug("api: starting a leader election campaign",
		"key", key, "holder", le.holder, "ttl", le.ttl)
	go l.campaign(runCtx)
	return l, nil
}

// leadership is a handle to one key's campaign.
type leadership struct {
	owner  *leaderElection
	key    string
	leader atomic.Bool
	cancel context.CancelFunc
	done   chan struct{}

	// leaseID is the claim the server handed back, kept so resign can name it.
	// Only the campaign goroutine writes it, and only after the campaign has
	// stopped does Close read it.
	leaseID string
}

func (l *leadership) IsLeader() bool { return l.leader.Load() }

// campaign asks for the key on a loop until the handle is closed.
//
// The interval depends on the answer: a leader renews often enough to keep the
// claim alive, and a challenger observes more slowly, because it is only waiting
// for the leader to stop.
func (l *leadership) campaign(ctx context.Context) {
	defer close(l.done)
	for ctx.Err() == nil {
		leader := l.claim(ctx)
		wait := l.owner.observe
		if leader {
			wait = l.owner.renew
		}
		if err := sleep(ctx, wait); err != nil {
			return
		}
	}
}

// claim makes one campaign call and records the answer, returning whether this
// replica now holds the key.
//
// A transport failure is read as "not the leader". That is the important line in
// this file: a leader that cannot reach the platform is not a leader, and it has
// to stop asserting it before its claim expires on the server, or two replicas
// will both believe they hold the key.
func (l *leadership) claim(ctx context.Context) bool {
	var out campaignResponse
	err := l.owner.c.json(ctx, routeLeaderCampaign,
		l.owner.c.url(routeLeaderCampaign, l.key),
		campaignRequest{Holder: l.owner.holder, TTLSeconds: seconds(l.owner.ttl)},
		&out, l.owner.c.timeout)
	if err != nil {
		if isNotImplemented(err) {
			l.owner.latch.mark()
		} else if ctx.Err() == nil {
			slog.Warn("api: a leadership campaign call failed; standing down until it succeeds",
				"key", l.key, "holder", l.owner.holder, "error", err)
		}
		l.transition(false)
		return false
	}
	if out.Leader {
		l.leaseID = out.LeaseID
	}
	l.transition(out.Leader)
	return out.Leader
}

// transition records the new status, logging only when it changed — a campaign
// loop that logged every poll would drown everything else.
func (l *leadership) transition(leader bool) {
	if was := l.leader.Swap(leader); was == leader {
		return
	}
	if leader {
		slog.Info("api: became the leader", "key", l.key, "holder", l.owner.holder)
		return
	}
	slog.Info("api: lost leadership", "key", l.key, "holder", l.owner.holder)
}

// Close stops the campaign, waits for it to wind down so no further IsLeader
// transition happens after it returns, and gives the key up.
func (l *leadership) Close() error {
	l.cancel()
	<-l.done
	if !l.leader.Load() || l.leaseID == "" {
		return nil
	}
	// Resigning is a courtesy that shortens the next replica's wait; the claim
	// expires on its own regardless, so a failure here is not worth surfacing.
	ctx, cancel := context.WithTimeout(context.Background(), resignTimeout)
	defer cancel()
	err := l.owner.c.json(ctx, routeLeaderResign, l.owner.c.url(routeLeaderResign, l.key),
		resignRequest{LeaseID: l.leaseID}, nil, resignTimeout)
	if err != nil {
		slog.Debug("api: could not resign leadership; the claim will expire instead",
			"key", l.key, "error", err)
	}
	l.leader.Store(false)
	return nil
}
