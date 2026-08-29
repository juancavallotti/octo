package api

import (
	"context"
	"testing"
	"time"
)

// fastElection is a discovery document whose campaign intervals are short enough
// to test without waiting.
func fastElection() discoveryDocument {
	doc := fullDiscovery()
	doc.Features.LeaderElection.LeaseTTLSeconds = 3
	doc.Features.LeaderElection.RenewIntervalSeconds = 1
	doc.Features.LeaderElection.ObserveIntervalSeconds = 1
	return doc
}

func newLeaderFixture(t *testing.T) (*Services, *leaderBackend) {
	t.Helper()
	f := newFake(t, fastElection())
	b := newLeaderBackend()
	b.install(f)
	return newTestServices(t, f, nil), b
}

// awaitLeader waits for IsLeader to reach want, or fails.
func awaitLeader(t *testing.T, l interface{ IsLeader() bool }, want bool, why string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if l.IsLeader() == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("IsLeader stayed %v: %s", l.IsLeader(), why)
}

// Acquire returns immediately and IsLeader converges, which is what
// core.LeaderElection promises: a false reading means "not yet".
func TestLeadershipConverges(t *testing.T) {
	svc, _ := newLeaderFixture(t)

	l, err := svc.LeaderElection().Acquire(t.Context(), "cron")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	awaitLeader(t, l, true, "the key was free, so the campaign should have won it")
}

// Exactly one replica holds a key. The second campaign observes rather than wins.
func TestOnlyOneReplicaLeads(t *testing.T) {
	svc, b := newLeaderFixture(t)
	b.take("cron", "someone-else", time.Minute)

	l, err := svc.LeaderElection().Acquire(t.Context(), "cron")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	time.Sleep(1500 * time.Millisecond)
	if l.IsLeader() {
		t.Fatal("two replicas both believe they hold the key")
	}
}

// A leader that cannot reach the platform stops asserting leadership, and must do
// so before its claim expires on the server — otherwise a successor and it would
// both believe they hold the key.
func TestLeaderStandsDownWhenUnreachable(t *testing.T) {
	svc, b := newLeaderFixture(t)

	l, err := svc.LeaderElection().Acquire(t.Context(), "cron")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	awaitLeader(t, l, true, "the key was free")

	b.setOffline(true)
	awaitLeader(t, l, false, "a leader that cannot reach the platform is not a leader")
}

// Leadership is regained once the key frees up, so a replica is not permanently
// demoted by having lost a round.
func TestLeadershipIsRegained(t *testing.T) {
	svc, b := newLeaderFixture(t)
	b.take("cron", "someone-else", 2*time.Second)

	l, err := svc.LeaderElection().Acquire(t.Context(), "cron")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	awaitLeader(t, l, true, "the other holder's claim expired, so this one should win it")
}

// Close stops the campaign and waits for it, so no IsLeader transition happens
// after it returns.
func TestLeadershipCloseStopsTheCampaign(t *testing.T) {
	svc, _ := newLeaderFixture(t)
	l, err := svc.LeaderElection().Acquire(t.Context(), "cron")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	awaitLeader(t, l, true, "the key was free")

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if l.IsLeader() {
		t.Fatal("IsLeader is still true after Close")
	}
}

// A renew interval too close to the TTL is a split brain waiting to happen, so it
// is shortened regardless of what the platform asked for.
func TestRenewIntervalClampedBelowTheTTL(t *testing.T) {
	le := newLeaderElection(t.Context(), nil, "me", leaderFeature{
		LeaseTTLSeconds: 15, RenewIntervalSeconds: 14, ObserveIntervalSeconds: 2,
	})
	if le.renew > le.ttl/renewIntervalDivisor {
		t.Fatalf("renew = %s with a ttl of %s; want at most a third of it", le.renew, le.ttl)
	}
}

// The defaults are client-go's, so failover timing does not change with the module.
func TestLeaderElectionDefaults(t *testing.T) {
	le := newLeaderElection(t.Context(), nil, "me", leaderFeature{})
	if le.ttl != defaultLeaderTTL || le.renew != defaultRenewInterval || le.observe != defaultObserveInterval {
		t.Fatalf("defaults = (ttl %s, renew %s, observe %s)", le.ttl, le.renew, le.observe)
	}
}

// Leadership must not outlive the campaign on ANY exit path.
//
// Close clears it, but a caller whose context is cancelled elsewhere leaves the
// campaign loop without going through Close — and nothing else was clearing the
// flag. IsLeader would stay true while no renewal was happening, so work gated
// on it would keep running while the claim expired on the platform and a second
// replica took the key.
func TestLeadershipClearsWhenItsContextIsCancelled(t *testing.T) {
	svc, _ := newLeaderFixture(t)
	ctx, cancel := context.WithCancel(t.Context())

	l, err := svc.LeaderElection().Acquire(ctx, "cron")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	awaitLeader(t, l, true, "the key was free")

	// Cancelled by whoever started it, with no Close in sight.
	cancel()
	awaitLeader(t, l, false, "leadership survived the campaign that was maintaining it")
}

// And Close still resigns after that clearing, rather than deciding there was
// nothing to give back.
func TestClosingAfterCancellationStillResigns(t *testing.T) {
	svc, b := newLeaderFixture(t)
	ctx, cancel := context.WithCancel(t.Context())

	l, err := svc.LeaderElection().Acquire(ctx, "cron")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	awaitLeader(t, l, true, "the key was free")
	cancel()
	awaitLeader(t, l, false, "leadership survived its campaign")

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, stillHeld := b.holders["cron"]; stillHeld {
		t.Fatal("Close did not resign the key it had been holding")
	}
}
