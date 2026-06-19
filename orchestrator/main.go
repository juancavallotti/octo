// Command orchestrator is a small HTTP API that sits alongside the editor and
// runtime in the local k3d dev cluster. This first iteration is intentionally
// minimal: a health check and a read of the db_version row seeded into
// site_settings by the schema Job. It exists so the cluster has a Go service
// wired to Postgres that we can grow real orchestration responsibilities into.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultPort     = "8090"
	shutdownTimeout = 10 * time.Second
	dbQueryTimeout  = 5 * time.Second
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("orchestrator stopped with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	port := envOr("PORT", defaultPort)
	dsn := os.Getenv("DATABASE_URL")

	// Root context cancelled on SIGINT/SIGTERM so k8s pod termination drains
	// cleanly rather than killing in-flight requests.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var pool *pgxpool.Pool
	if dsn == "" {
		// The service still serves /healthz without a database, which keeps it
		// useful for liveness probes before Postgres is reachable.
		slog.Warn("DATABASE_URL is not set; /db-version will report the DB as unavailable")
	} else {
		p, err := pgxpool.New(ctx, dsn)
		if err != nil {
			return err
		}
		defer p.Close()
		pool = p
		slog.Info("connected to database pool")
	}

	srv := newServer(pool)
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("orchestrator listening", "addr", httpServer.Addr,
			"db", dsn != "", "endpoints", "/healthz /db-version")
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

// newServer wires the routes. pool may be nil when DATABASE_URL is unset.
func newServer(pool *pgxpool.Pool) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /db-version", func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			http.Error(w, "database not configured", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), dbQueryTimeout)
		defer cancel()

		// site_settings.value is jsonb; scan it straight into raw JSON and pass it
		// through unmodified so callers see exactly what was seeded.
		var value json.RawMessage
		err := pool.QueryRow(ctx,
			"SELECT value FROM site_settings WHERE key = $1", "db_version",
		).Scan(&value)
		if err != nil {
			slog.Error("db-version query failed", "error", err)
			http.Error(w, "failed to read db_version", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(value)
	})

	return mux
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
