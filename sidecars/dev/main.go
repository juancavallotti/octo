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
// It speaks to exactly one peer, the orchestrator: outbound for the bundle, and
// inbound for commands once the command API lands. It never touches the Kubernetes
// API, so a dev pod carries no cluster credential and can reach nothing beyond its
// own integration's data.
//
// This commit adds the pull: the orchestrator client, the coordinator that
// serialises reloads, and the startup pull that populates the workspace. The HTTP
// command surface that lets the orchestrator ask for a reload arrives next.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/juancavallotti/octo/sidecars/dev/internal/bundle"
	"github.com/juancavallotti/octo/sidecars/dev/internal/reload"
	"github.com/juancavallotti/octo/sidecars/dev/internal/workspace"
)

const (
	defaultPort = "8099"
	// defaultWorkspaceDir is the directory the runtime container watches. It names
	// the watched directory directly rather than a root to join onto, so the two
	// containers share one value with no implicit path arithmetic between them.
	defaultWorkspaceDir = "/workspace/integrations"
	// shutdownTimeout bounds how long in-flight requests have to drain on SIGTERM.
	shutdownTimeout = 10 * time.Second
	// readHeaderTimeout bounds time spent reading request headers, mitigating
	// slow-header denial-of-service attempts.
	readHeaderTimeout = 10 * time.Second
	// expireGrace bounds the shutdown that follows the dev run being expired. Short:
	// the run is gone, so there is nothing left worth draining for.
	expireGrace = 5 * time.Second
	// firstPullRetryDelay paces the startup pull's retries.
	firstPullRetryDelay = 2 * time.Second
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

// config is everything the sidecar reads from its environment.
type config struct {
	port         string
	workspaceDir string
	orchestrator string
	devRunID     string
	devRunToken  string
}

// loadConfig reads and validates the environment.
//
// Every missing required value is a hard startup failure, and they are listed
// together so one restart reveals all of them. Deliberately unlike the log
// aggregator, which degrades to serving /healthz without a database: that service
// is still useful half-configured and this one is not. A sidecar that cannot reach
// the orchestrator, or cannot prove which dev run it is, has no job to do — and
// CrashLoopBackOff with a named cause is a far better signal than a pod that looks
// healthy and silently never populates its workspace.
func loadConfig() (config, error) {
	cfg := config{
		port:         envOr("PORT", defaultPort),
		workspaceDir: envOr("WORKSPACE_DIR", defaultWorkspaceDir),
		orchestrator: os.Getenv("ORCHESTRATOR_URL"),
		devRunID:     os.Getenv("DEV_RUN_ID"),
		devRunToken:  os.Getenv("DEV_RUN_TOKEN"),
	}

	var missing []string
	for _, req := range []struct {
		name  string
		value string
	}{
		{"ORCHESTRATOR_URL", cfg.orchestrator},
		{"DEV_RUN_ID", cfg.devRunID},
		{"DEV_RUN_TOKEN", cfg.devRunToken},
	} {
		if req.value == "" {
			missing = append(missing, req.name)
		}
	}
	if len(missing) > 0 {
		return config{}, fmt.Errorf("missing required environment: %v", missing)
	}
	return cfg, nil
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Root context cancelled on SIGINT/SIGTERM so pod termination drains cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Before anything else, and before the runtime container is allowed to start.
	// `octo run --watch` survives a missing config but not a missing DIRECTORY: its
	// watcher's fsnotify Add fails and the process exits (runtime/octo/watch.go:29).
	// So an empty workspace is a working starting point and an absent one is a
	// crash-loop — which is why this is a hard startup failure rather than something
	// the first pull gets around to.
	ws := workspace.New(cfg.workspaceDir)
	if err := ws.Ensure(); err != nil {
		return err
	}

	client := bundle.NewClient(cfg.orchestrator, cfg.devRunID, cfg.devRunToken)
	coordinator := reload.New(client, ws)

	httpServer := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           newServer(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("dev sidecar listening",
			"addr", httpServer.Addr,
			"devRun", cfg.devRunID,
			"workspace", ws.Dir(),
		)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// The first pull, in the background so a slow or unreachable orchestrator cannot
	// stop the sidecar from serving its probes.
	go firstPull(ctx, coordinator)

	select {
	case err := <-errCh:
		return err
	case <-coordinator.Expired():
		// The dev run is gone and the orchestrator has been told to tear it down. Exit
		// rather than sit here serving an integration nobody can account for; the
		// workload is being deleted underneath us either way.
		slog.Warn("dev run expired, shutting down", "devRun", cfg.devRunID)
		return shutdown(httpServer, expireGrace)
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

// firstPull populates the workspace at startup, retrying until it succeeds, the run
// is expired, or the process is shutting down.
//
// Retrying rather than failing is the right shape here: at pod start the
// orchestrator may still be rolling, and a sidecar that gave up would leave the
// runtime watching an empty directory with nothing to fix it. The one non-retryable
// outcome — the dev run being gone — closes Expired() from inside the coordinator,
// which run() is selecting on.
func firstPull(ctx context.Context, coordinator *reload.Coordinator) {
	for attempt := 1; ; attempt++ {
		_, err := coordinator.Reload(ctx)
		switch {
		case err == nil:
			slog.Info("workspace populated", "attempt", attempt)
			return
		case errors.Is(err, bundle.ErrGone):
			// Terminal. The coordinator has already asked the orchestrator to expire the
			// run and closed Expired(), so logging "retrying" here would describe
			// something that is not going to happen.
			slog.Warn("dev run is gone; not retrying", "attempt", attempt, "error", err)
			return
		default:
			slog.Warn("first pull failed, retrying",
				"attempt", attempt, "error", err, "in", firstPullRetryDelay)
		}

		select {
		case <-ctx.Done():
			return
		case <-coordinator.Expired():
			return
		case <-time.After(firstPullRetryDelay):
		}
	}
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
