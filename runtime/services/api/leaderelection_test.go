package api

import (
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
	le := newLeaderElection(nil, "me", leaderFeature{
		LeaseTTLSeconds: 15, RenewIntervalSeconds: 14, ObserveIntervalSeconds: 2,
	})
	if le.renew > le.ttl/renewIntervalDivisor {
		t.Fatalf("renew = %s with a ttl of %s; want at most a third of it", le.renew, le.ttl)
	}
}

// The defaults are client-go's, so failover timing does not change with the module.
func TestLeaderElectionDefaults(t *testing.T) {
	le := newLeaderElection(nil, "me", leaderFeature{})
	if le.ttl != defaultLeaderTTL || le.renew != defaultRenewInterval || le.observe != defaultObserveInterval {
		t.Fatalf("defaults = (ttl %s, renew %s, observe %s)", le.ttl, le.renew, le.observe)
	}
}
