package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestVerifyAcceptsAGoodToken(t *testing.T) {
	idp := newFakeIDP(t)
	v := NewVerifier(idp.Issuer(), idp.clientID)

	token := idp.idToken(t, tokenOptions{
		subject: "provider|abc123", email: "first@example.com", name: "First Person",
	})

	got, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	want := Identity{Subject: "provider|abc123", Email: "first@example.com", Name: "First Person"}
	if got != want {
		t.Errorf("Verify() = %+v, want %+v", got, want)
	}
}

// Each case bends exactly one thing about an otherwise-correct token, so a
// rejection is attributable to the check it is named after.
func TestVerifyRejects(t *testing.T) {
	idp := newFakeIDP(t)

	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}

	tests := []struct {
		name string
		opts tokenOptions
	}{
		{
			"a token for another application at the same provider",
			tokenOptions{subject: "s", email: "a@example.com", audience: "someone-elses-app"},
		},
		{
			"a token from another issuer",
			tokenOptions{subject: "s", email: "a@example.com", issuer: "https://evil.example"},
		},
		{
			"an expired token",
			tokenOptions{subject: "s", email: "a@example.com", expiry: time.Now().Add(-time.Minute)},
		},
		{
			"a token signed by a key the provider does not publish",
			tokenOptions{subject: "s", email: "a@example.com", signWith: otherKey},
		},
		{
			// The email keys nothing, but the platform has no way to show a user who
			// they are signed in as without it, and the user row requires one.
			"a token with no email claim",
			tokenOptions{subject: "s"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A fresh verifier per case, so one case's cached discovery cannot mask
			// another's failure.
			v := NewVerifier(idp.Issuer(), idp.clientID)
			_, err := v.Verify(context.Background(), idp.idToken(t, tt.opts))
			if !errors.Is(err, ErrUnauthenticated) {
				t.Errorf("Verify() error = %v, want ErrUnauthenticated", err)
			}
		})
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	idp := newFakeIDP(t)
	v := NewVerifier(idp.Issuer(), idp.clientID)

	for _, token := range []string{"", "not-a-token", "a.b.c"} {
		if _, err := v.Verify(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("Verify(%q) error = %v, want ErrUnauthenticated", token, err)
		}
	}
}

// A provider that is down must be reported as our problem, not as a bad token —
// telling a caller their credential was rejected would send them to
// re-authenticate against a server that cannot answer.
func TestVerifyReportsAnUnreachableProviderSeparately(t *testing.T) {
	v := NewVerifier("http://127.0.0.1:1/nothing-here", "octo-platform")

	_, err := v.Verify(context.Background(), "irrelevant")
	if !errors.Is(err, ErrProviderUnreachable) {
		t.Errorf("Verify() error = %v, want ErrProviderUnreachable", err)
	}
	if errors.Is(err, ErrUnauthenticated) {
		t.Error("an unreachable provider was reported as a rejected token")
	}
}

// A failed discovery must not be remembered, or a provider that was down while
// this pod started stays "down" until the pod is restarted.
func TestVerifyRetriesDiscoveryAfterAFailure(t *testing.T) {
	idp := newFakeIDP(t)

	// Pointed at nothing first, then at the real provider — standing in for a
	// provider that was unreachable and then came back.
	v := NewVerifier("http://127.0.0.1:1/nothing-here", idp.clientID)
	if _, err := v.Verify(context.Background(), "irrelevant"); !errors.Is(err, ErrProviderUnreachable) {
		t.Fatalf("Verify() error = %v, want ErrProviderUnreachable", err)
	}
	if v.cached() != nil {
		t.Fatal("a failed discovery was cached")
	}

	v.issuer = idp.Issuer()
	token := idp.idToken(t, tokenOptions{subject: "s", email: "a@example.com"})
	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Errorf("Verify() after the provider came back: %v", err)
	}
}

// Discovery is a request to somebody else's server; doing it on every exchange
// would put the provider in the hot path of every sign-in.
func TestVerifyDiscoversOnce(t *testing.T) {
	idp := newFakeIDP(t)
	v := NewVerifier(idp.Issuer(), idp.clientID)
	ctx := context.Background()

	for range 3 {
		token := idp.idToken(t, tokenOptions{subject: "s", email: "a@example.com"})
		if _, err := v.Verify(ctx, token); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	}
	if v.cached() == nil {
		t.Error("the resolved verifier was not retained")
	}
	if got := idp.discoveryCount(); got != 1 {
		t.Errorf("the provider was asked for its configuration %d times, want 1", got)
	}
}

// Discovery happens outside the mutex, so concurrent callers cannot queue behind
// one another. Several may discover at once before the first result is stored —
// that is the accepted cost — but once one has, every later caller must reuse it
// rather than asking again.
func TestConcurrentVerifiesSettleOnOneVerifier(t *testing.T) {
	idp := newFakeIDP(t)
	v := NewVerifier(idp.Issuer(), idp.clientID)
	ctx := context.Background()

	// Warm it, so the count below measures only what the concurrent callers add.
	if _, err := v.Verify(ctx, idp.idToken(t, tokenOptions{subject: "s", email: "a@example.com"})); err != nil {
		t.Fatalf("Verify(warm): %v", err)
	}
	warm := idp.discoveryCount()

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		token := idp.idToken(t, tokenOptions{subject: "s", email: "a@example.com"})
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := v.Verify(ctx, token); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Verify: %v", err)
	}

	if got := idp.discoveryCount(); got != warm {
		t.Errorf("a warm verifier discovered %d more times, want 0", got-warm)
	}
}
