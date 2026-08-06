package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A pod configured wrong must fail loudly and name every missing value, so one
// restart tells the operator everything rather than one thing at a time.
func TestLoadConfigRequiresEnv(t *testing.T) {
	for _, name := range []string{"ORCHESTRATOR_URL", "DEV_RUN_ID", "DEV_RUN_TOKEN"} {
		t.Setenv(name, "")
	}

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig: want an error when nothing is configured")
	}
	for _, name := range []string{"ORCHESTRATOR_URL", "DEV_RUN_ID", "DEV_RUN_TOKEN"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("ORCHESTRATOR_URL", "http://orchestrator:8090")
	t.Setenv("DEV_RUN_ID", "run-1")
	t.Setenv("DEV_RUN_TOKEN", "tok")
	t.Setenv("PORT", "")
	t.Setenv("WORKSPACE_DIR", "")

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

// Driven through the real mux the service builds, so the route pattern and its
// method matching are part of what is tested.
func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	// Probes are polled constantly and the answer changes with the process's state.
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}
