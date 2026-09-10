package main

import (
	"testing"
	"time"

	"github.com/juancavallotti/octo/iam/internal/auth"
	"github.com/juancavallotti/octo/iam/internal/signing"
)

// A duration that is mistyped rather than omitted has to stop startup naming the
// setting: "60" and "1hour" are silently unparseable, and a coerced value would
// surface much later as tokens with the wrong lifetime.
func TestSigningConfigRejectsAMalformedDuration(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"seconds without a unit", "IAM_TOKEN_TTL", "60"},
		{"a unit that is not one", "IAM_TOKEN_TTL", "1hour"},
		{"zero", "IAM_TOKEN_TTL", "0s"},
		{"negative", "IAM_TOKEN_TTL", "-1h"},
		{"key lifetime too", "IAM_KEY_LIFETIME", "thirty days"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearSigningEnv(t)
			t.Setenv(tt.key, tt.value)
			if _, err := signingConfig(); err == nil {
				t.Errorf("signingConfig() with %s=%q returned no error", tt.key, tt.value)
			}
		})
	}
}

// Unset durations must stay zero, which is what the signing package reads as
// "use your own defaults" — coercing them here would move the defaults out of
// the package that documents them.
func TestSigningConfigLeavesUnsetDurationsAtZero(t *testing.T) {
	clearSigningEnv(t)
	t.Setenv("IAM_ISSUER", "https://iam.example")

	cfg, err := signingConfig()
	if err != nil {
		t.Fatalf("signingConfig(): %v", err)
	}
	if cfg.TokenTTL != 0 || cfg.KeyLifetime != 0 {
		t.Errorf("durations = %v/%v, want both zero", cfg.TokenTTL, cfg.KeyLifetime)
	}
	if cfg.Issuer != "https://iam.example" {
		t.Errorf("issuer = %q, want the configured one", cfg.Issuer)
	}
	if cfg.Audience != defaultAudience {
		t.Errorf("audience = %q, want the default %q", cfg.Audience, defaultAudience)
	}
}

func TestSigningConfigReadsDurations(t *testing.T) {
	clearSigningEnv(t)
	t.Setenv("IAM_ISSUER", "https://iam.example")
	t.Setenv("IAM_AUDIENCE", "octo-staging")
	t.Setenv("IAM_TOKEN_TTL", "15m")
	t.Setenv("IAM_KEY_LIFETIME", "168h")

	cfg, err := signingConfig()
	if err != nil {
		t.Fatalf("signingConfig(): %v", err)
	}
	if cfg.TokenTTL != 15*time.Minute {
		t.Errorf("TokenTTL = %v, want 15m", cfg.TokenTTL)
	}
	if cfg.KeyLifetime != 168*time.Hour {
		t.Errorf("KeyLifetime = %v, want 168h", cfg.KeyLifetime)
	}
	if cfg.Audience != "octo-staging" {
		t.Errorf("audience = %q, want the configured one", cfg.Audience)
	}
}

// signingConfig reads the process environment, which under `go test` is whatever
// the developer's shell holds. Cleared explicitly so an inherited IAM_* cannot
// change an answer.
func clearSigningEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"IAM_ISSUER", "IAM_AUDIENCE", "IAM_TOKEN_TTL", "IAM_KEY_LIFETIME",
	} {
		t.Setenv(key, "")
	}
}

func TestRefreshGrace(t *testing.T) {
	tests := []struct {
		name    string
		set     string
		want    time.Duration
		wantErr bool
	}{
		{"unset falls back to the package default", "", auth.DefaultRefreshGrace, false},
		{"a duration is taken as given", "2m", 2 * time.Minute, false},
		{"the keyset's own margin is the ceiling", signing.MaxRefreshGrace.String(), signing.MaxRefreshGrace, false},
		// The likely typos: a bare number, and an English-looking unit. Both would
		// otherwise mean something quite different from the intent, or nothing.
		{"a bare number is refused", "600", 0, true},
		{"an invented unit is refused", "10minutes", 0, true},
		{"zero is refused", "0s", 0, true},
		{"a negative duration is refused", "-1m", 0, true},
		// Past this the keyset stops publishing the key a token was signed with
		// while the token is still inside its window, so the promise would hold for
		// most tokens and break for the ones minted near a rotation.
		{"longer than the keyset can honour is refused", (signing.MaxRefreshGrace + time.Minute).String(), 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("IAM_REFRESH_GRACE", tt.set)
			got, err := refreshGrace()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("refreshGrace() = %v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("refreshGrace(): %v", err)
			}
			if got != tt.want {
				t.Errorf("refreshGrace() = %v, want %v", got, tt.want)
			}
		})
	}
}
