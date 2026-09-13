package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"time"

	cryptox "github.com/juancavallotti/octo/iam/internal/crypto"
	"github.com/juancavallotti/octo/iam/internal/db"
	"github.com/juancavallotti/octo/iam/internal/signing"
)

const (
	// defaultAudience is the `aud` every platform token carries. One value, because
	// what a token may reach is a question for its roles rather than its audience.
	defaultAudience = "octo"
)

// signingConfig reads the settings that shape a minted token.
//
// IAM_ISSUER has no default: it is the `iss` claim and the base a caller's
// discovery lands on, and a wrong value produces tokens that verify nowhere without
// any failure naming this setting. signing.NewService refuses an empty one.
//
// The two durations are optional and left at zero when unset, which the signing
// service reads as "use your own defaults" so that one place owns each default. A
// malformed value stops startup naming the setting, because the likely typos
// ("60", "1hour") differ from the intent by a factor nothing would show.
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
// The variable is KV_ENCRYPTION_KEY: every service that seals data at rest reads
// that same key, so an install has one thing to hold and one to rotate.
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
// Both are reported the same way: neither is a fault, and in both cases POST /auth
// reports itself unavailable rather than the process refusing to start.
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
