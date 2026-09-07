package cooldown

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// A real server rather than a fake, because what is under test is the atomicity
// of SET NX and the TTL semantics — the two things a fake would simply agree
// with. Set REDIS_TEST_URL to run it; without one these skip, on the same terms
// the fold's own store tests do. The database is FLUSHED, so it must be a
// disposable one — see the guard below.
func storeFor(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL is not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("REDIS_TEST_URL: %v", err)
	}
	// FlushDB is about to delete everything in the selected database, so refuse
	// one that was not asked for. Database 0 is what a bare redis:// URL selects
	// and what a developer's own Redis is almost certainly using; requiring a
	// non-zero index makes pointing REDIS_TEST_URL at something real an explicit
	// act rather than an accident. The same guard podstats already applies.
	if opts.DB == 0 {
		t.Fatalf("refusing to flush database 0 of %s: point REDIS_TEST_URL at a "+
			"disposable database, e.g. redis://127.0.0.1:63799/9", url)
	}
	client := redis.NewClient(opts)
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return New(client)
}

func TestTheFirstClaimWinsAndTheRestDoNot(t *testing.T) {
	s := storeFor(t)

	first, err := s.Begin(t.Context(), "w_1", time.Minute)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !first {
		t.Fatal("the first claim was refused")
	}
	second, err := s.Begin(t.Context(), "w_1", time.Minute)
	if err != nil {
		t.Fatalf("begin again: %v", err)
	}
	if second {
		t.Error("a second claim inside the window was allowed")
	}
}

// One key per watch: a cooldown on one must not quieten another.
func TestWatchesDoNotShareACooldown(t *testing.T) {
	s := storeFor(t)

	if ok, _ := s.Begin(t.Context(), "w_1", time.Minute); !ok {
		t.Fatal("the first claim was refused")
	}
	ok, err := s.Begin(t.Context(), "w_2", time.Minute)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !ok {
		t.Error("one watch's cooldown silenced another")
	}
}

func TestTheClaimLapses(t *testing.T) {
	s := storeFor(t)

	if ok, _ := s.Begin(t.Context(), "w_1", 50*time.Millisecond); !ok {
		t.Fatal("the first claim was refused")
	}
	time.Sleep(120 * time.Millisecond)

	ok, err := s.Begin(t.Context(), "w_1", time.Minute)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !ok {
		t.Error("the claim outlived its ttl")
	}
}

// No cooldown configured is not a cooldown of zero length — it must not write a
// key at all, or every watch would accumulate one.
func TestNoTTLClaimsNothing(t *testing.T) {
	s := storeFor(t)

	for range 3 {
		ok, err := s.Begin(t.Context(), "w_1", 0)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if !ok {
			t.Fatal("a watch with no cooldown was silenced")
		}
	}
	if _, held, _ := s.Until(t.Context(), "w_1"); held {
		t.Error("a watch with no cooldown holds a key")
	}
}

func TestUntilReportsWhenItLifts(t *testing.T) {
	s := storeFor(t)

	if _, held, _ := s.Until(t.Context(), "w_1"); held {
		t.Error("an unclaimed watch reported a cooldown")
	}
	if ok, _ := s.Begin(t.Context(), "w_1", time.Hour); !ok {
		t.Fatal("the claim was refused")
	}
	until, held, err := s.Until(t.Context(), "w_1")
	if err != nil {
		t.Fatalf("until: %v", err)
	}
	if !held || until.Before(time.Now().UTC().Add(50*time.Minute)) {
		t.Errorf("cooldown lifts at %v, held=%v", until, held)
	}
}

// For somebody who has dealt with the thing and wants to be told again.
func TestClearLetsItSpeakAgain(t *testing.T) {
	s := storeFor(t)

	if ok, _ := s.Begin(t.Context(), "w_1", time.Hour); !ok {
		t.Fatal("the claim was refused")
	}
	if err := s.Clear(t.Context(), "w_1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	ok, err := s.Begin(t.Context(), "w_1", time.Hour)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !ok {
		t.Error("a cleared cooldown still silenced the watch")
	}
}

// A process with no Redis enforces no cooldown, which is the honest failure: the
// alternative is silently suppressing every alert on the installation.
func TestWithoutARedisNothingIsSuppressed(t *testing.T) {
	var s *Store
	ok, err := s.Begin(context.Background(), "w_1", time.Hour)
	if err != nil || !ok {
		t.Errorf("Begin = (%v, %v), want (true, nil)", ok, err)
	}
	if _, held, err := s.Until(context.Background(), "w_1"); held || err != nil {
		t.Errorf("Until = (%v, %v), want (false, nil)", held, err)
	}
	if err := s.Clear(context.Background(), "w_1"); err != nil {
		t.Errorf("Clear = %v, want nil", err)
	}
}
