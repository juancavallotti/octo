package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/juancavallotti/octo/iam/internal/auth"
	cryptox "github.com/juancavallotti/octo/iam/internal/crypto"
	"github.com/juancavallotti/octo/iam/internal/db"
	"github.com/juancavallotti/octo/iam/internal/signing"
)

const (
	// defaultAudience is the `aud` every platform token carries. One value,
	// because there is one platform: a per-service audience would mean a token
	// that lets you call the orchestrator and not the observability service, which
	// is what roles are for.
	defaultAudience = "octo"
)

// signingConfig reads the settings that shape a minted token.
//
// IAM_ISSUER has no default. It is the `iss` claim and the base a caller's
// discovery lands on, so a wrong value produces tokens that verify nowhere and a
// discovery document pointing at somebody else — and neither failure names this
// setting when it happens. Required as soon as the service can mint at all;
// signing.NewService is what refuses an empty one.
//
// The two durations are optional and left at zero when unset, which the signing
// service reads as "use your own defaults" rather than as a value: one place owns
// each default, and it is the package that documents it. A malformed value stops
// startup naming the setting, because the likely typos ("60", "1hour") differ
// from the intent by a factor nobody would notice from behaviour.
func signingConfig() (signing.Config, error) {
	cfg := signing.Config{
		Issuer:   os.Getenv("IAM_ISSUER"),
		Audience: envOr("IAM_AUDIENCE", defaultAudience),
	}
	for _, d := range []struct {
		name string
		into *time.Duration
	}{
		{"IAM_TOKEN_TTL", &cfg.TokenTTL},
		{"IAM_KEY_LIFETIME", &cfg.KeyLifetime},
	} {
		raw := os.Getenv(d.name)
		if raw == "" {
			continue
		}
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return signing.Config{}, fmt.Errorf("parse %s: %q is not a positive duration", d.name, raw)
		}
		*d.into = parsed
	}
	return cfg, nil
}

// newCipher builds the at-rest encryption cipher from a base64-encoded key. An
// empty key returns a nil cipher, which leaves token signing disabled; a malformed
// key or an invalid length stops startup.
//
// The variable is KV_ENCRYPTION_KEY, shared with the orchestrator, and the name is
// the orchestrator's history rather than a description — it protects rather more
// than KV there too. What matters is that both services read the SAME key, so this
// platform has one thing to hold and one to rotate.
func newCipher(b64 string) (*cryptox.Cipher, error) {
	if b64 == "" {
		return nil, nil //nolint:nilnil // no key means signing is off, not an error
	}
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode KV_ENCRYPTION_KEY: %w", err)
	}
	return cryptox.NewCipher(key)
}

// newSigningService builds the keyset, or reports ErrInvalidConfig when this
// install has not configured one — an absent issuer, or an absent encryption key.
//
// Both are reported the same way for the same reason: neither is a fault, both are
// coherent ways to run, and in both cases what happens is that POST /auth reports
// itself unavailable rather than the process refusing to start.
func newSigningService(
	database *db.DB, cipher *cryptox.Cipher, cfg signing.Config,
) (*signing.Service, error) {
	if cipher == nil {
		return nil, fmt.Errorf(
			"%w: KV_ENCRYPTION_KEY is not set, and signing keys are not stored unencrypted",
			signing.ErrInvalidConfig)
	}
	repo, err := signing.NewRepo(database.Pool(), cipher)
	if err != nil {
		return nil, err
	}
	return signing.NewService(repo, cfg)
}

// refreshGrace reads how long past its expiry a platform token can still be
// traded for a fresh one, falling back to the auth package's own default.
//
// Optional and parsed the same way the signing durations are, for the same
// reason: the likely typos differ from the intent by a factor nobody would
// notice from behaviour, so a malformed value stops startup naming the setting
// rather than quietly meaning something else.
func refreshGrace() (time.Duration, error) {
	raw := os.Getenv("IAM_REFRESH_GRACE")
	if raw == "" {
		return auth.DefaultRefreshGrace, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("parse IAM_REFRESH_GRACE: %q is not a positive duration", raw)
	}
	return parsed, nil
}
