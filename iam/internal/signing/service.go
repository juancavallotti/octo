package signing

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
)

const (
	// DefaultTokenTTL is how long a minted platform token is valid. An hour: long
	// enough that nothing re-authenticates constantly, short enough that a
	// revoked role takes effect within a working session.
	DefaultTokenTTL = time.Hour
	// DefaultKeyLifetime is how long a key signs before a new one takes over.
	DefaultKeyLifetime = 30 * 24 * time.Hour

	// signingAlgorithm is ES256 for every key. One algorithm rather than a
	// setting: a second would have to be verifiable everywhere the first is, for
	// no gain that anyone has asked for.
	signingAlgorithm = string(jose.ES256)

	// gracePeriod is how much longer than one token lifetime a retired key stays
	// published. Without it a token minted in the last instant before rotation
	// would expire exactly as its key stopped verifying, which is a race decided
	// by clock skew between two machines.
	gracePeriod = time.Hour
)

// repository is the persistence surface the service needs. Declared in the
// consumer (and unexported) so service tests can substitute a fake; *Repo
// satisfies it structurally.
type repository interface {
	Current(ctx context.Context, now time.Time) (Key, error)
	Verifiers(ctx context.Context, now time.Time) ([]Key, error)
	Rotate(ctx context.Context, now time.Time, generate func() (Key, error)) (Key, error)
}

// Config is what the service needs to stamp and size a token.
type Config struct {
	// Issuer is the `iss` claim and the URL this service is reachable at, which
	// is what makes the published discovery document consistent with the tokens.
	Issuer string
	// Audience is the `aud` claim: who the token is for.
	Audience string
	// TokenTTL and KeyLifetime take their defaults when zero.
	TokenTTL    time.Duration
	KeyLifetime time.Duration
}

// Service mints platform tokens and publishes the keys that verify them.
type Service struct {
	repo repository
	cfg  Config
	// now is swappable so the tests can stand at either side of a rotation
	// boundary rather than sleeping through one.
	now func() time.Time
}

// NewService returns a Service backed by repo. An issuer or audience that is not
// set is refused here rather than producing tokens no one can check: `iss` is
// what a verifier matches against the discovery document it fetched, so an empty
// one is a token that fails at the far end for a reason nothing here would report.
func NewService(repo repository, cfg Config) (*Service, error) {
	// A trailing slash is trimmed rather than accepted, because the issuer is used
	// three ways that have to agree byte for byte: it is the `iss` claim, it is
	// what the discovery document reports, and it is the base jwks_uri is built
	// on. "https://iam.example/" would advertise a doubled slash in the URI and
	// stamp the slash into every token, and a verifier comparing `iss` against its
	// own configured issuer would reject them for a reason nothing names.
	cfg.Issuer = strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/")
	if cfg.Issuer == "" {
		return nil, fmt.Errorf("%w: an issuer is required", ErrInvalidConfig)
	}
	if cfg.Audience == "" {
		return nil, fmt.Errorf("%w: an audience is required", ErrInvalidConfig)
	}
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = DefaultTokenTTL
	}
	if cfg.KeyLifetime <= 0 {
		cfg.KeyLifetime = DefaultKeyLifetime
	}
	if cfg.KeyLifetime <= cfg.TokenTTL {
		// Otherwise a key retires before the tokens it signed do, and every
		// rotation strands whatever was minted in the overlap.
		return nil, fmt.Errorf("%w: the key lifetime (%s) must exceed the token lifetime (%s)",
			ErrInvalidConfig, cfg.KeyLifetime, cfg.TokenTTL)
	}
	return &Service{repo: repo, cfg: cfg, now: time.Now}, nil
}

// Issuer returns the configured issuer, which the discovery handler renders.
func (s *Service) Issuer() string { return s.cfg.Issuer }

// Audience returns the configured audience.
func (s *Service) Audience() string { return s.cfg.Audience }

// TokenTTL returns how long a minted token lives.
func (s *Service) TokenTTL() time.Duration { return s.cfg.TokenTTL }

// Mint signs a token for subject, merging private into the registered claims.
//
// The registered claims belong to this service — it is the one that knows the
// issuer, the audience and the lifetime — and everything about *who* the subject
// is belongs to the caller, which is why the second half arrives as an opaque
// struct rather than as fields here.
func (s *Service) Mint(ctx context.Context, subject string, private any) (Token, error) {
	key, err := s.signingKey(ctx)
	if err != nil {
		return Token{}, err
	}

	priv, err := x509.ParsePKCS8PrivateKey(key.Private)
	if err != nil {
		return Token{}, fmt.Errorf("signing: parse stored private key %s: %w", key.KID, err)
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.SignatureAlgorithm(key.Algorithm), Key: priv},
		// The kid rides in the header so a verifier picks the right key out of the
		// JWKS instead of trying each in turn.
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), key.KID),
	)
	if err != nil {
		return Token{}, fmt.Errorf("signing: new signer: %w", err)
	}

	now := s.now()
	expiry := now.Add(s.cfg.TokenTTL)
	registered := jwt.Claims{
		Issuer:   s.cfg.Issuer,
		Subject:  subject,
		Audience: jwt.Audience{s.cfg.Audience},
		IssuedAt: jwt.NewNumericDate(now),
		// NotBefore is stamped a little early so a verifier whose clock is behind
		// ours does not reject a token that was just minted.
		NotBefore: jwt.NewNumericDate(now.Add(-clockSkew)),
		Expiry:    jwt.NewNumericDate(expiry),
		ID:        uuid.NewString(),
	}

	value, err := jwt.Signed(signer).Claims(registered).Claims(private).Serialize()
	if err != nil {
		return Token{}, fmt.Errorf("signing: serialize token: %w", err)
	}
	return Token{Value: value, ExpiresAt: expiry}, nil
}

// clockSkew is how far back a token's not-before is stamped, and the tolerance a
// verifier should allow. Two machines in one cluster are not perfectly in step,
// and a token rejected for being from the future is the least diagnosable
// possible failure.
const clockSkew = 30 * time.Second

// JWKS returns the public half of every key that has not expired, as the document
// served at /.well-known/jwks.json. Retired keys are in it on purpose: they no
// longer sign, but tokens they signed are still inside their lifetime.
func (s *Service) JWKS(ctx context.Context) (jose.JSONWebKeySet, error) {
	keys, err := s.repo.Verifiers(ctx, s.now())
	if err != nil {
		return jose.JSONWebKeySet{}, err
	}

	// A brand-new install has no key until the first token is minted, and a caller
	// fetching an empty JWKS would cache "this issuer has no keys". Minting one
	// here means the set is populated as soon as anything asks.
	if len(keys) == 0 {
		fresh, err := s.signingKey(ctx)
		if err != nil {
			return jose.JSONWebKeySet{}, err
		}
		keys = []Key{fresh}
	}

	out := jose.JSONWebKeySet{Keys: make([]jose.JSONWebKey, 0, len(keys))}
	for _, k := range keys {
		pub, err := x509.ParsePKIXPublicKey(k.Public)
		if err != nil {
			return jose.JSONWebKeySet{}, fmt.Errorf("signing: parse stored public key %s: %w", k.KID, err)
		}
		out.Keys = append(out.Keys, jose.JSONWebKey{
			Key:       pub,
			KeyID:     k.KID,
			Algorithm: k.Algorithm,
			Use:       "sig",
		})
	}
	return out, nil
}

// signingKey returns the key to sign with, rotating when the current one has
// retired — or when there has never been one, which is the same question with the
// same answer and so is deliberately not a separate path.
func (s *Service) signingKey(ctx context.Context) (Key, error) {
	now := s.now()
	key, err := s.repo.Current(ctx, now)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, ErrNoKey) {
		return Key{}, err
	}
	return s.repo.Rotate(ctx, now, func() (Key, error) { return s.generate(now) })
}

// generate mints a fresh ES256 keypair with its two horizons set.
func (s *Service) generate(now time.Time) (Key, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Key{}, fmt.Errorf("signing: generate key: %w", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return Key{}, fmt.Errorf("signing: marshal private key: %w", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(priv.Public())
	if err != nil {
		return Key{}, fmt.Errorf("signing: marshal public key: %w", err)
	}
	kid, err := thumbprint(priv.Public())
	if err != nil {
		return Key{}, err
	}

	retireAfter := now.Add(s.cfg.KeyLifetime)
	return Key{
		KID:         kid,
		Algorithm:   signingAlgorithm,
		Private:     privDER,
		Public:      pubDER,
		RetireAfter: retireAfter,
		// One token lifetime past retirement, plus a margin: the last token this
		// key signs is minted an instant before retireAfter and lives a full TTL
		// beyond that.
		ExpiresAt: retireAfter.Add(s.cfg.TokenTTL + gracePeriod),
	}, nil
}

// thumbprint derives the key id from the key itself (RFC 7638), so a kid cannot
// name a different key than the one it was computed from.
func thumbprint(pub crypto.PublicKey) (string, error) {
	sum, err := (&jose.JSONWebKey{Key: pub}).Thumbprint(crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("signing: key thumbprint: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(sum), nil
}
