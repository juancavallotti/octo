// Command logs is the log-aggregator service. It consumes log events shipped by
// deployed runtimes over the internal.logs NATS subject (as a competing consumer)
// and persists them to Postgres for the platform's /logs view to query.
//
// This scaffold establishes the service lifecycle: configuration from the
// environment, a Postgres pool, a health endpoint, and graceful shutdown on
// SIGINT/SIGTERM. The NATS consumer and the query API are added in later changes.
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

	"github.com/juancavallotti/octo/logs/internal/api"
	"github.com/juancavallotti/octo/logs/internal/db"
	"github.com/juancavallotti/octo/logs/internal/ingest"
	"github.com/juancavallotti/octo/logs/internal/repo"
	"github.com/nats-io/nats.go"
)

const (
	defaultPort = "8091"
	// defaultWorkers bounds concurrent inserts feeding off the NATS subscription.
	defaultWorkers = 8
	// shutdownTimeout bounds how long in-flight HTTP requests have to drain when a
	// termination signal arrives.
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
		slog.Error("log aggregator stopped with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	port := envOr("PORT", defaultPort)
	dsn := os.Getenv("DATABASE_URL")

	// Root context cancelled on SIGINT/SIGTERM so pod termination drains cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var database *db.DB
	if dsn == "" {
		// The service still serves /healthz without a database, keeping it useful for
		// liveness probes before Postgres is reachable.
		slog.Warn("DATABASE_URL is not set; the log store is unavailable")
	} else {
		d, err := db.New(ctx, dsn)
		if err != nil {
			return err
		}
		defer d.Close()
		database = d
		slog.Info("connected to database pool")
	}

	// Start the NATS consumer when both a store and a broker are configured. Without
	// either, the service still serves /healthz so liveness probes pass while the
	// dependencies come up.
	natsURL := os.Getenv("NATS_URL")
	switch {
	case database == nil:
		slog.Warn("DATABASE_URL is not set; not consuming logs")
	case natsURL == "":
		slog.Warn("NATS_URL is not set; not consuming logs")
	default:
		conn, err := nats.Connect(natsURL, nats.Name("octo-logs"))
		if err != nil {
			return fmt.Errorf("connect nats %q: %w", natsURL, err)
		}
		defer conn.Close()

		consumer := ingest.NewLogConsumer(repo.NewLogs(database.Pool()), defaultWorkers)
		sub, err := consumer.Start(ctx, conn)
		if err != nil {
			return err
		}
		defer func() { _ = sub.Close() }()
		slog.Info("consuming logs", "subject", ingest.LogSubject, "nats", natsURL)
	}

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           newServer(database),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("log aggregator listening", "addr", httpServer.Addr, "db", database != nil)
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

// newServer wires the HTTP routes. The log query API is registered only when a
// database is configured; /healthz always serves so liveness probes pass even
// before Postgres is reachable.
func newServer(database *db.DB) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	if database != nil {
		api.NewHandler(repo.NewLogs(database.Pool())).Register(mux)
		slog.Info("log query API registered", "endpoint", "GET /logs")
	}
	return mux
}

// envOr returns the value of key, or fallback when it is empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseLevel maps a LOG_LEVEL name to an slog.Level, defaulting to info. It
// matches the runtime's accepted level names so operators configure both alike.
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
