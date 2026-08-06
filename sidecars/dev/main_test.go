package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
