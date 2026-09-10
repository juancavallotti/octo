package main

import (
	"fmt"
	"os"
	"time"

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
