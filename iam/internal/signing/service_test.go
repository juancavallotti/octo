package signing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// memRepo is a hand-written in-memory keyset for the service tests. It keeps the
// two properties the real one gets from Postgres — Current only returns a key
// that has not retired, Verifiers returns everything unexpired — so what these
// tests exercise is the rotation policy rather than a fake that agrees with it.
type memRepo struct {
	keys []Key
	// rotations counts how many times a key was actually generated, which is what
	// distinguishes "rotated" from "reused the existing one".
	rotations int
	failNext  error
}

func (m *memRepo) fail() error {
	err := m.failNext
	m.failNext = nil
	return err
}

func (m *memRepo) Current(_ context.Context, now time.Time) (Key, error) {
	if err := m.fail(); err != nil {
		return Key{}, err
	}
	var (
		best  Key
		found bool
	)
	for _, k := range m.keys {
		if k.RetireAfter.After(now) && (!found || k.RetireAfter.After(best.RetireAfter)) {
			best, found = k, true
		}
	}
	if !found {
		return Key{}, ErrNoKey
	}
	return best, nil
}

func (m *memRepo) Verifiers(_ context.Context, now time.Time) ([]Key, error) {
	if err := m.fail(); err != nil {
		return nil, err
	}
	out := make([]Key, 0, len(m.keys))
	for _, k := range m.keys {
		if k.ExpiresAt.After(now) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (m *memRepo) Rotate(ctx context.Context, now time.Time, generate func() (Key, error)) (Key, error) {
	if err := m.fail(); err != nil {
		return Key{}, err
	}
	// The real one rechecks under a lock, so the fake rechecks too — otherwise a
	// test could pass here and rotate twice against Postgres.
	if existing, err := m.Current(ctx, now); err == nil {
		return existing, nil
	}
	fresh, err := generate()
	if err != nil {
		return Key{}, err
	}
	m.rotations++
	kept := make([]Key, 0, len(m.keys)+1)
	for _, k := range m.keys {
		if k.ExpiresAt.After(now) {
			kept = append(kept, k)
		}
	}
	m.keys = append(kept, fresh)
	return fresh, nil
}

// privateClaims is what the auth package will merge in — modelled here so the
// merge is covered without this package depending on it.
type privateClaims struct {
	Email string   `json:"email"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

func newTestService(t *testing.T, cfg Config) (*Service, *memRepo) {
	t.Helper()
	repo := &memRepo{}
	if cfg.Issuer == "" {
		cfg.Issuer = "http://iam.test"
	}
	if cfg.Audience == "" {
		cfg.Audience = "octo"
	}
	svc, err := NewService(repo, cfg)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, repo
}

// verifyAgainstJWKS checks a token the way another service must: fetch the
// published key set, pick the key the header names, and verify with that alone.
// Nothing in the service is used to check its own output.
func verifyAgainstJWKS(t *testing.T, svc *Service, token string) (jwt.Claims, privateClaims) {
	t.Helper()
	set, err := svc.JWKS(context.Background())
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}

	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if len(parsed.Headers) != 1 {
		t.Fatalf("token has %d headers, want 1", len(parsed.Headers))
	}
	kid := parsed.Headers[0].KeyID
	if kid == "" {
		t.Fatal("token carries no kid; a verifier would have to try every key")
	}
	matches := set.Key(kid)
	if len(matches) != 1 {
		t.Fatalf("JWKS holds %d keys for kid %q, want 1", len(matches), kid)
	}

	var (
		registered jwt.Claims
		private    privateClaims
	)
	if err := parsed.Claims(matches[0].Key, &registered, &private); err != nil {
		t.Fatalf("verify with the published key: %v", err)
	}
	return registered, private
}

func TestMintedTokenVerifiesAgainstThePublishedJWKS(t *testing.T) {
	svc, _ := newTestService(t, Config{})
	want := privateClaims{
		Email: "first@example.com",
		Name:  "First",
		Roles: []string{"platform:admin"},
	}

	tok, err := svc.Mint(context.Background(), "user-1", want)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	registered, private := verifyAgainstJWKS(t, svc, tok.Value)

	if registered.Issuer != svc.Issuer() {
		t.Errorf("iss = %q, want %q", registered.Issuer, svc.Issuer())
	}
	if registered.Subject != "user-1" {
		t.Errorf("sub = %q, want %q", registered.Subject, "user-1")
	}
	if !registered.Audience.Contains(svc.Audience()) {
		t.Errorf("aud = %v, want it to contain %q", registered.Audience, svc.Audience())
	}
	if registered.ID == "" {
		t.Error("jti is empty")
	}
	if private.Email != want.Email || private.Name != want.Name {
		t.Errorf("private claims = %+v, want %+v", private, want)
	}
	if len(private.Roles) != 1 || private.Roles[0] != "platform:admin" {
		t.Errorf("roles = %v, want [platform:admin]", private.Roles)
	}

	// The expiry the caller is handed must be the one in the token, or a client
	// caching by it will refresh at the wrong moment.
	if got := registered.Expiry.Time(); !got.Equal(tok.ExpiresAt.Truncate(time.Second)) {
		t.Errorf("token exp = %v, want the reported %v", got, tok.ExpiresAt)
	}
}

// Every mint before the key retires must reuse it. Rotating per request would
// publish an unbounded key set and invalidate nothing usefully.
func TestMintReusesTheCurrentKey(t *testing.T) {
	svc, repo := newTestService(t, Config{})
	ctx := context.Background()

	for range 3 {
		if _, err := svc.Mint(ctx, "user-1", privateClaims{}); err != nil {
			t.Fatalf("Mint: %v", err)
		}
	}
	if repo.rotations != 1 {
		t.Errorf("generated %d keys for three mints, want 1", repo.rotations)
	}
}

// The whole point of the two horizons: a token minted just before rotation must
// still verify after it, because its key is retired but still published.
func TestATokenSurvivesTheRotationThatRetiresItsKey(t *testing.T) {
	svc, repo := newTestService(t, Config{TokenTTL: time.Hour, KeyLifetime: 24 * time.Hour})
	ctx := context.Background()

	// The key is generated lazily on the first mint, so its lifetime is measured
	// from here and not from some earlier moment.
	start := time.Now()
	svc.now = func() time.Time { return start }
	if _, err := svc.Mint(ctx, "user-1", privateClaims{}); err != nil {
		t.Fatalf("Mint(first): %v", err)
	}

	// Minted one second before that key retires — the worst case the grace period
	// exists for.
	svc.now = func() time.Time { return start.Add(24*time.Hour - time.Second) }
	tok, err := svc.Mint(ctx, "user-1", privateClaims{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	firstKID := kidOf(t, tok.Value)

	// Past the retirement: a new key takes over for signing.
	svc.now = func() time.Time { return start.Add(24*time.Hour + time.Minute) }
	next, err := svc.Mint(ctx, "user-1", privateClaims{})
	if err != nil {
		t.Fatalf("Mint(after rotation): %v", err)
	}
	if secondKID := kidOf(t, next.Value); secondKID == firstKID {
		t.Error("the same key signed after its retirement")
	}
	if repo.rotations != 2 {
		t.Errorf("rotations = %d, want 2", repo.rotations)
	}

	// And the first token still verifies, because the retired key is still
	// published. The library's own expiry check is bypassed here on purpose: what
	// is under test is that the key is available, not the clock.
	set, err := svc.JWKS(ctx)
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}
	if len(set.Key(firstKID)) != 1 {
		t.Errorf("the retired key %q is no longer published; the token it signed cannot be checked",
			firstKID)
	}
}

// Once every token a key could have signed has expired, the key must leave the
// set — otherwise it accumulates forever and the JWKS grows without bound.
func TestAnExpiredKeyIsDroppedFromTheJWKS(t *testing.T) {
	svc, _ := newTestService(t, Config{TokenTTL: time.Hour, KeyLifetime: 24 * time.Hour})
	ctx := context.Background()

	start := time.Now()
	svc.now = func() time.Time { return start }
	tok, err := svc.Mint(ctx, "user-1", privateClaims{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	firstKID := kidOf(t, tok.Value)

	// Retirement plus the token lifetime plus the grace period, and a minute more.
	svc.now = func() time.Time {
		return start.Add(24*time.Hour + time.Hour + gracePeriod + time.Minute)
	}
	if _, err := svc.Mint(ctx, "user-1", privateClaims{}); err != nil {
		t.Fatalf("Mint(much later): %v", err)
	}

	set, err := svc.JWKS(ctx)
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}
	if len(set.Key(firstKID)) != 0 {
		t.Errorf("the expired key %q is still published", firstKID)
	}
	if len(set.Keys) != 1 {
		t.Errorf("JWKS holds %d keys, want just the current one", len(set.Keys))
	}
}

// A caller that fetches the keys before anything has signed must not be handed an
// empty set and cache it.
func TestJWKSOnAFreshInstallMintsAKey(t *testing.T) {
	svc, repo := newTestService(t, Config{})

	set, err := svc.JWKS(context.Background())
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}
	if len(set.Keys) != 1 {
		t.Fatalf("JWKS on a fresh install holds %d keys, want 1", len(set.Keys))
	}
	if repo.rotations != 1 {
		t.Errorf("rotations = %d, want 1", repo.rotations)
	}
	if set.Keys[0].KeyID == "" {
		t.Error("published key has no kid")
	}
	if set.Keys[0].Algorithm != signingAlgorithm {
		t.Errorf("published alg = %q, want %q", set.Keys[0].Algorithm, signingAlgorithm)
	}
	if set.Keys[0].Use != "sig" {
		t.Errorf("published use = %q, want sig", set.Keys[0].Use)
	}
}

// The JWKS must carry public keys only. Publishing the private half would hand
// anyone who can reach the endpoint the ability to mint platform tokens.
func TestJWKSPublishesNoPrivateKeyMaterial(t *testing.T) {
	svc, _ := newTestService(t, Config{})

	set, err := svc.JWKS(context.Background())
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}
	for _, k := range set.Keys {
		if !k.IsPublic() {
			t.Errorf("key %q is published with its private half", k.KeyID)
		}
	}
}

// The kid is the key's own RFC 7638 thumbprint, so it must be reproducible from
// the published public key alone.
func TestKIDIsTheThumbprintOfThePublishedKey(t *testing.T) {
	svc, _ := newTestService(t, Config{})

	set, err := svc.JWKS(context.Background())
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}
	for _, k := range set.Keys {
		want, err := thumbprint(k.Key)
		if err != nil {
			t.Fatalf("thumbprint: %v", err)
		}
		if k.KeyID != want {
			t.Errorf("kid = %q, want the thumbprint %q", k.KeyID, want)
		}
	}
}

func TestNewServiceRejectsIncoherentConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"no issuer", Config{Audience: "octo"}},
		{"no audience", Config{Issuer: "http://iam.test"}},
		{
			"a key that retires before its tokens do",
			Config{Issuer: "http://iam.test", Audience: "octo",
				TokenTTL: 2 * time.Hour, KeyLifetime: time.Hour},
		},
		{
			"a key lifetime equal to the token lifetime",
			Config{Issuer: "http://iam.test", Audience: "octo",
				TokenTTL: time.Hour, KeyLifetime: time.Hour},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewService(&memRepo{}, tt.cfg); !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("NewService() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestNewServiceAppliesDefaults(t *testing.T) {
	svc, err := NewService(&memRepo{}, Config{Issuer: "http://iam.test", Audience: "octo"})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if svc.TokenTTL() != DefaultTokenTTL {
		t.Errorf("TokenTTL() = %v, want %v", svc.TokenTTL(), DefaultTokenTTL)
	}
	if svc.cfg.KeyLifetime != DefaultKeyLifetime {
		t.Errorf("KeyLifetime = %v, want %v", svc.cfg.KeyLifetime, DefaultKeyLifetime)
	}
}

// A database fault must surface as an error, not as an empty key set — a caller
// that cached an empty JWKS would reject every token until it refreshed.
func TestJWKSSurfacesAReadFailure(t *testing.T) {
	svc, repo := newTestService(t, Config{})
	boom := errors.New("connection reset")
	repo.failNext = boom

	if _, err := svc.JWKS(context.Background()); !errors.Is(err, boom) {
		t.Errorf("JWKS() error = %v, want the repository's error", err)
	}
}

func kidOf(t *testing.T, token string) string {
	t.Helper()
	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	return parsed.Headers[0].KeyID
}

// The issuer is the `iss` claim, what the discovery document reports, and the
// base jwks_uri is built on. A trailing slash in one and not the others is a
// mismatch a verifier reports as an issuer it does not recognise.
func TestNewServiceNormalizesTheIssuer(t *testing.T) {
	for _, given := range []string{
		"https://iam.example/", "https://iam.example//", " https://iam.example/ ",
	} {
		svc, err := NewService(&memRepo{}, Config{Issuer: given, Audience: "octo"})
		if err != nil {
			t.Fatalf("NewService(%q): %v", given, err)
		}
		if got := svc.Issuer(); got != "https://iam.example" {
			t.Errorf("Issuer() = %q for input %q, want %q", got, given, "https://iam.example")
		}
	}

	// A slash-only issuer is still no issuer.
	if _, err := NewService(&memRepo{}, Config{Issuer: "///", Audience: "octo"}); err == nil {
		t.Error("NewService(\"///\") returned no error")
	}
}

// --- Verify ----------------------------------------------------------------

// The ordinary case a proactive re-mint takes: the token is still valid and no
// window is needed.
func TestVerifyAcceptsAValidToken(t *testing.T) {
	svc, _ := newTestService(t, Config{})
	ctx := context.Background()

	token, err := svc.Mint(ctx, "user-1", privateClaims{Email: "a@example.com"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	claims, err := svc.Verify(ctx, token.Value, 0, nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Errorf("sub = %q, want user-1", claims.Subject)
	}
}

// The window is what lets somebody who stepped away from a tab come back to a
// working session instead of a sign-in page.
func TestVerifyAcceptsATokenExpiredWithinTheWindow(t *testing.T) {
	svc, _ := newTestService(t, Config{})
	ctx := context.Background()

	start := time.Now()
	svc.now = func() time.Time { return start }
	token, err := svc.Mint(ctx, "user-1", privateClaims{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	// A minute past expiry, with ten minutes of grace.
	svc.now = func() time.Time { return start.Add(svc.TokenTTL() + time.Minute) }
	if _, err := svc.Verify(ctx, token.Value, 10*time.Minute, nil); err != nil {
		t.Errorf("Verify() inside the window: %v", err)
	}
}

// And past it there is nothing left to trade.
func TestVerifyRejectsATokenExpiredBeyondTheWindow(t *testing.T) {
	svc, _ := newTestService(t, Config{})
	ctx := context.Background()

	start := time.Now()
	svc.now = func() time.Time { return start }
	token, err := svc.Mint(ctx, "user-1", privateClaims{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	svc.now = func() time.Time { return start.Add(svc.TokenTTL() + 11*time.Minute) }
	if _, err := svc.Verify(ctx, token.Value, 10*time.Minute, nil); !errors.Is(err, ErrNotOurToken) {
		t.Errorf("Verify() error = %v, want ErrNotOurToken", err)
	}
}

// A window of zero must not quietly become a window of some: an expired token is
// simply expired for every caller but the refresh.
func TestVerifyWithNoWindowRejectsAnExpiredToken(t *testing.T) {
	svc, _ := newTestService(t, Config{})
	ctx := context.Background()

	start := time.Now()
	svc.now = func() time.Time { return start }
	token, err := svc.Mint(ctx, "user-1", privateClaims{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	svc.now = func() time.Time { return start.Add(svc.TokenTTL() + time.Minute) }
	if _, err := svc.Verify(ctx, token.Value, 0, nil); !errors.Is(err, ErrNotOurToken) {
		t.Errorf("Verify() error = %v, want ErrNotOurToken", err)
	}
}

// The window widens expiry and nothing else. Everything that says "this is not
// our token" still says it, however recently the token was minted.
func TestVerifyRejectsTokensThatAreNotOurs(t *testing.T) {
	svc, _ := newTestService(t, Config{})
	other, _ := newTestService(t, Config{Issuer: "http://elsewhere.test", Audience: "someone-else"})
	ctx := context.Background()

	foreign, err := other.Mint(ctx, "user-1", privateClaims{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	// Minted by a service with its own keyset, so both the signature and the
	// issuer are wrong — which is what a token from anywhere else looks like.
	if _, err := svc.Verify(ctx, foreign.Value, time.Hour, nil); !errors.Is(err, ErrNotOurToken) {
		t.Errorf("Verify(another issuer's token) error = %v, want ErrNotOurToken", err)
	}
	for _, garbage := range []string{"", "not-a-token", "a.b.c"} {
		if _, err := svc.Verify(ctx, garbage, time.Hour, nil); !errors.Is(err, ErrNotOurToken) {
			t.Errorf("Verify(%q) error = %v, want ErrNotOurToken", garbage, err)
		}
	}
}

// A token minted just before a rotation must stay refreshable through it: the
// retired key is still published for exactly this reason.
func TestVerifyAcceptsATokenSignedByARetiredKey(t *testing.T) {
	svc, _ := newTestService(t, Config{TokenTTL: time.Hour, KeyLifetime: 2 * time.Hour})
	ctx := context.Background()

	start := time.Now()
	svc.now = func() time.Time { return start }
	token, err := svc.Mint(ctx, "user-1", privateClaims{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	// Past the key's retirement, so a later mint rotates to a new one, but inside
	// both the old key's publication window and the token's own refresh window.
	svc.now = func() time.Time { return start.Add(2*time.Hour + time.Minute) }
	if _, err := svc.Mint(ctx, "user-2", privateClaims{}); err != nil {
		t.Fatalf("Mint after rotation: %v", err)
	}

	if _, err := svc.Verify(ctx, token.Value, 2*time.Hour, nil); err != nil {
		t.Errorf("Verify() of a token signed by the retired key: %v", err)
	}
}

// A window forgives a late expiry and must not tighten anything else. Mint
// stamps `nbf` a little in the past for clock skew, so validating both horizons
// against one instant moved back by the window makes a token minted a second ago
// read as "not valid yet" — which is how this broke the first time.
func TestVerifyWithAWindowStillAcceptsABrandNewToken(t *testing.T) {
	svc, _ := newTestService(t, Config{})
	ctx := context.Background()

	token, err := svc.Mint(ctx, "user-1", privateClaims{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if _, err := svc.Verify(ctx, token.Value, time.Hour, nil); err != nil {
		t.Errorf("Verify() of a freshly minted token with an hour of grace: %v", err)
	}
}

// The regression test for the bug the fake clock hid.
//
// Verify used to hand issuer and audience to jwt.Claims.ValidateWithLeeway with
// no Expected.Time, and go-jose falls back to time.Now() when that is zero — the
// real clock, not this service's. Every other test here moves svc.now forward,
// which leaves the token unexpired by the real clock, so the validator passed it
// through and the window check below did the work. In production the two clocks
// are the same and the validator refused every expired token before the window
// was ever consulted, which would have made the whole refresh grace dead code.
//
// So this one mints in the past instead of verifying in the future: the token is
// genuinely expired by the wall clock, exactly as it would be in production.
func TestVerifyForgivesAnExpiryAgainstTheRealClock(t *testing.T) {
	svc, _ := newTestService(t, Config{TokenTTL: time.Hour, KeyLifetime: 24 * time.Hour})
	ctx := context.Background()

	// Minted two hours ago, so it expired an hour ago by any clock.
	svc.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	token, err := svc.Mint(ctx, "user-1", privateClaims{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	svc.now = time.Now
	if _, err := svc.Verify(ctx, token.Value, 2*time.Hour, nil); err != nil {
		t.Errorf("Verify() of a genuinely expired token inside the window: %v", err)
	}
	if _, err := svc.Verify(ctx, token.Value, 0, nil); !errors.Is(err, ErrNotOurToken) {
		t.Errorf("Verify() with no window accepted an expired token: %v", err)
	}
}
