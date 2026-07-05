package http

import "testing"

func TestResolveHost(t *testing.T) {
	t.Run("explicit setting wins over env", func(t *testing.T) {
		t.Setenv(envHTTPHost, "10.0.0.1")
		if got := resolveHost("192.168.0.1"); got != "192.168.0.1" {
			t.Fatalf("resolveHost = %q, want the explicit setting", got)
		}
	})
	t.Run("env used when setting unset", func(t *testing.T) {
		t.Setenv(envHTTPHost, "10.0.0.1")
		if got := resolveHost(""); got != "10.0.0.1" {
			t.Fatalf("resolveHost = %q, want the env value", got)
		}
	})
	t.Run("defaults to all interfaces", func(t *testing.T) {
		// Ensure the env var is absent for this subtest.
		t.Setenv(envHTTPHost, "")
		if got := resolveHost(""); got != defaultHost {
			t.Fatalf("resolveHost = %q, want %q", got, defaultHost)
		}
	})
}

func TestResolvePort(t *testing.T) {
	explicit := 3000
	zero := 0
	t.Run("explicit setting wins over env", func(t *testing.T) {
		t.Setenv(envHTTPPort, "9999")
		if got := resolvePort(&explicit); got != explicit {
			t.Fatalf("resolvePort = %d, want the explicit setting", got)
		}
	})
	t.Run("explicit 0 preserved (OS-assigned)", func(t *testing.T) {
		t.Setenv(envHTTPPort, "9999")
		if got := resolvePort(&zero); got != 0 {
			t.Fatalf("resolvePort = %d, want 0 (OS-assigned)", got)
		}
	})
	t.Run("env used when setting unset", func(t *testing.T) {
		t.Setenv(envHTTPPort, "9090")
		if got := resolvePort(nil); got != 9090 {
			t.Fatalf("resolvePort = %d, want the env value", got)
		}
	})
	t.Run("unparseable env ignored, falls back to default", func(t *testing.T) {
		t.Setenv(envHTTPPort, "not-a-port")
		if got := resolvePort(nil); got != defaultPort {
			t.Fatalf("resolvePort = %d, want %d", got, defaultPort)
		}
	})
	t.Run("defaults when unset", func(t *testing.T) {
		t.Setenv(envHTTPPort, "")
		if got := resolvePort(nil); got != defaultPort {
			t.Fatalf("resolvePort = %d, want %d", got, defaultPort)
		}
	})
}
