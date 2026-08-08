package main

import (
	"log/slog"
	"strings"
	"testing"
)

// requiredEnv is what a dev pod must supply. Listed once so the two tests below
// cannot drift apart, and so adding a required value forces a decision here.
var requiredEnv = []string{"ORCHESTRATOR_URL", "DEV_RUN_ID", "DEV_RUN_TOKEN", "SIDECAR_TOKEN"}

// A pod configured wrong must fail loudly and name every missing value, so one
// restart tells the operator everything rather than one thing at a time.
func TestLoadConfigRequiresEnv(t *testing.T) {
	for _, name := range requiredEnv {
		t.Setenv(name, "")
	}

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig: want an error when nothing is configured")
	}
	for _, name := range requiredEnv {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

// Each required value is checked on its own, so a check that silently stopped
// covering one of them fails here rather than shipping a pod that starts without
// the credential it needs.
func TestLoadConfigRequiresEachValue(t *testing.T) {
	for _, missing := range requiredEnv {
		t.Run(missing, func(t *testing.T) {
			for _, name := range requiredEnv {
				t.Setenv(name, "value")
			}
			t.Setenv(missing, "")

			_, err := loadConfig()
			if err == nil {
				t.Fatalf("loadConfig: want an error when %s is unset", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("error %q does not name the missing %s", err, missing)
			}
		})
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	for _, name := range requiredEnv {
		t.Setenv(name, "value")
	}
	t.Setenv("PORT", "")
	t.Setenv("WORKSPACE_DIR", "")
	t.Setenv("RUNTIME_ADMIN_ADDR", "")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.port != defaultPort {
		t.Errorf("port = %q, want %q", cfg.port, defaultPort)
	}
	// The default names the watched directory itself, so nothing has to join a root
	// onto a subpath and get it subtly different from the runtime container's.
	if cfg.workspaceDir != defaultWorkspaceDir {
		t.Errorf("workspaceDir = %q, want %q", cfg.workspaceDir, defaultWorkspaceDir)
	}
	// Loopback, because the two containers share a network namespace — so the admin
	// port needs no Service, no credential and no exposure.
	if cfg.runtimeAdmin != defaultRuntimeAdmin {
		t.Errorf("runtimeAdmin = %q, want %q", cfg.runtimeAdmin, defaultRuntimeAdmin)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name    string
		want    slog.Level
		wantErr bool
	}{
		{"", slog.LevelInfo, false},
		{"info", slog.LevelInfo, false},
		{"debug", slog.LevelDebug, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		// An unrecognised level defaults to info AND reports why, so a typo in a values
		// file surfaces as a warning rather than silently changing what gets logged.
		{"verbose", slog.LevelInfo, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLevel(tc.name)
			if got != tc.want {
				t.Errorf("level = %v, want %v", got, tc.want)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("OCTO_TEST_ENV", "set")
	if got := envOr("OCTO_TEST_ENV", "fallback"); got != "set" {
		t.Errorf("envOr = %q, want set", got)
	}
	t.Setenv("OCTO_TEST_ENV", "")
	if got := envOr("OCTO_TEST_ENV", "fallback"); got != "fallback" {
		t.Errorf("envOr = %q, want fallback — an empty value is not a value", got)
	}
}

// The probes moved out of this file and into internal/api, which owns the whole
// HTTP surface and tests it through a real ServeMux. What is left here is the
// wiring main.go itself decides: what the environment must supply.
