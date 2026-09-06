package main

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// setEnv applies a set of environment variables for one test, clearing every
// variable loadConfig reads first so a test never inherits another's.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range []string{
		"PORT", "RUNTIME_ADMIN_ADDR", "REDIS_URL", "DEPLOYMENT_ID", "POD_NAME",
		"STATS_SAMPLE_INTERVAL", "STATS_ROLLUP_INTERVAL", "STATS_RETENTION",
	} {
		t.Setenv(k, "")
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
}

// valid is the minimum environment a sidecar starts with.
func valid() map[string]string {
	return map[string]string{
		"REDIS_URL":     "redis://localhost:6379",
		"DEPLOYMENT_ID": "dep-1",
		"POD_NAME":      "octo-dep-1-abc",
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	setEnv(t, valid())

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	tests := []struct {
		name string
		got  any
		want any
	}{
		{"port", cfg.port, defaultPort},
		{"runtime admin", cfg.runtimeAdmin, defaultRuntimeAdmin},
		{"sample interval", cfg.sample, time.Second},
		{"rollup interval", cfg.rollup, time.Hour},
		{"retention", cfg.retention, 7 * 24 * time.Hour},
		// An hour of one-second samples, and a week of hourly buckets.
		{"live depth", cfg.liveDepth(), int64(3600)},
		{"rollup depth", cfg.rollupDepth(), int64(168)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("= %v, want %v", tc.got, tc.want)
			}
		})
	}
}

// The knob that matters: a finer rollup gives more history rows without
// touching anything else.
func TestLoadConfigQuarterHourRollup(t *testing.T) {
	env := valid()
	env["STATS_ROLLUP_INTERVAL"] = "15m"
	setEnv(t, env)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got := cfg.liveDepth(); got != 900 {
		t.Errorf("live depth = %d, want 900", got)
	}
	if got := cfg.rollupDepth(); got != 672 {
		t.Errorf("rollup depth = %d, want 672 (a week of quarter hours)", got)
	}
}

// Every problem is reported at once, so one restart reveals all of them rather
// than one per crash loop.
func TestLoadConfigReportsEveryProblemTogether(t *testing.T) {
	setEnv(t, map[string]string{"STATS_SAMPLE_INTERVAL": "banana"})

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{
		"REDIS_URL", "DEPLOYMENT_ID", "POD_NAME", "STATS_SAMPLE_INTERVAL",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestLoadConfigRejects(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "unparseable duration",
			env:  map[string]string{"STATS_ROLLUP_INTERVAL": "1 hour"},
			want: "is not a duration",
		},
		{
			// Silently falling back to the default would make an experiment
			// produce a result about the wrong configuration.
			name: "zero duration",
			env:  map[string]string{"STATS_SAMPLE_INTERVAL": "0s"},
			want: "must be positive",
		},
		{
			name: "negative duration",
			env:  map[string]string{"STATS_RETENTION": "-1h"},
			want: "must be positive",
		},
		{
			name: "sample rate below the floor",
			env:  map[string]string{"STATS_SAMPLE_INTERVAL": "1ms"},
			want: "below the",
		},
		{
			// Otherwise the live tier's depth is zero and nothing is kept.
			name: "rollup not longer than sample",
			env:  map[string]string{"STATS_SAMPLE_INTERVAL": "1s", "STATS_ROLLUP_INTERVAL": "1s"},
			want: "must be longer than STATS_SAMPLE_INTERVAL",
		},
		{
			name: "retention not longer than rollup",
			env:  map[string]string{"STATS_ROLLUP_INTERVAL": "1h", "STATS_RETENTION": "30m"},
			want: "must be longer than STATS_ROLLUP_INTERVAL",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := valid()
			for k, v := range tc.env {
				env[k] = v
			}
			setEnv(t, env)

			_, err := loadConfig()
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadConfigMissingRequired(t *testing.T) {
	for _, missing := range []string{"REDIS_URL", "DEPLOYMENT_ID", "POD_NAME"} {
		t.Run(missing, func(t *testing.T) {
			env := valid()
			delete(env, missing)
			setEnv(t, env)

			_, err := loadConfig()
			if err == nil {
				t.Fatalf("expected an error when %s is unset", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("error %q does not name %s", err, missing)
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"", slog.LevelInfo, false},
		{"info", slog.LevelInfo, false},
		{"debug", slog.LevelDebug, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"loud", slog.LevelInfo, true},
	}
	for _, tc := range tests {
		t.Run("level "+tc.in, func(t *testing.T) {
			got, err := parseLevel(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("level = %v, want %v", got, tc.want)
			}
		})
	}
}
