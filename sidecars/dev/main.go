// Command dev-sidecar owns the workspace of a platform dev run: the pod the
// orchestrator creates when someone clicks Run in the editor. It shares an
// emptyDir with an `octo run --config <dir> --watch` container and does the two
// jobs that container cannot do for itself — it pulls the integration's definition
// and resources from the orchestrator into the watched directory, and it answers
// questions about the runtime beside it over HTTP.
//
// Why this exists at all: the editor's RUN state used to live in the platform
// BFF's memory — a child process handle, a log buffer, an allocated port — while
// the BFF runs with several replicas and no session affinity. Any request that
// landed on a different replica than the one that started the run found nothing.
// Moving the running app into its own pod is what makes every replica able to
// serve every request, because none of them holds anything.
//
// It will speak to exactly one peer, the orchestrator: inbound for commands,
// outbound for the bundle. It never touches the Kubernetes API, so a dev pod
// carries no cluster credential and can reach nothing beyond its own
// integration's data.
//
// This commit establishes the service lifecycle — configuration from the
// environment, a health endpoint, and graceful shutdown on SIGINT/SIGTERM. The
// workspace, the orchestrator client and the command API arrive in later changes.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	defaultPort = "8099"
	// shutdownTimeout bounds how long in-flight requests have to drain on SIGTERM.
	shutdownTimeout = 10 * time.Second
	// readHeaderTimeout bounds time spent reading request headers, mitigating
	// slow-header denial-of-service attempts.
	readHeaderTimeout = 10 * time.Second
)

func main() {
	level, levelErr := parseLevel(os.Getenv("LOG_LEVEL"))
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	if levelErr != nil {
		slog.Warn("invalid LOG_LEVEL, defaulting to info", "error", levelErr)
	}

	if err := run(); err != nil {
		slog.Error("dev sidecar stopped with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	port := envOr("PORT", defaultPort)

	// Root context cancelled on SIGINT/SIGTERM so pod termination drains cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           newServer(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("dev sidecar listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining")
		return shutdown(httpServer, shutdownTimeout)
	}
}

// newServer wires the HTTP routes.
//
// /healthz is unconditional, for the same reason the runtime's own is
// (runtime/services/observability/probes.go:37): liveness asks whether the process
// is wedged, and this handler running at all is the answer. Gating it on a
// dependency being reachable would restart a perfectly healthy sidecar every time
// that dependency rolled.
func newServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// A cached probe answer is worse than no answer: the whole value is that it
		// reflects the state right now.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

// shutdown drains the server within the given grace period.
func shutdown(server *http.Server, grace time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

// envOr returns the value of key, or fallback when it is empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseLevel maps a LOG_LEVEL name to an slog.Level, defaulting to info. It
// matches the names the runtime and the other services accept, so an operator
// configures every octo process alike.
func parseLevel(name string) (slog.Level, error) {
	switch name {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, errors.New("log level is not one of debug/info/warn/error")
	}
}
