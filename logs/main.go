// Command logs is the telemetry aggregator. It consumes what deployed runtimes
// ship over NATS as a competing consumer — log records on internal.logs, trace
// records on internal.traces — and persists both to Postgres for the platform to
// query.
//
// It is still called "logs" everywhere: the binary, the image, the pod and the
// LOGS_URL other services reach it by. Traces landed here because they are the
// same shape of problem and splitting the service would have been a deployment
// change rather than a design one; the naming that follows from that is tracked
// as its own issue rather than pretended away.
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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/juancavallotti/octo/logs/internal/api"
	"github.com/juancavallotti/octo/logs/internal/cost"
	"github.com/juancavallotti/octo/logs/internal/db"
	"github.com/juancavallotti/octo/logs/internal/ingest"
	"github.com/juancavallotti/octo/logs/internal/repo"
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

	// Start the NATS consumers when both a store and a broker are configured.
	// Without either, the service still serves /healthz so liveness probes pass
	// while the dependencies come up.
	natsURL := os.Getenv("NATS_URL")
	switch {
	case database == nil:
		slog.Warn("DATABASE_URL is not set; not consuming telemetry")
	case natsURL == "":
		slog.Warn("NATS_URL is not set; not consuming telemetry")
	default:
		conn, err := nats.Connect(natsURL, nats.Name("octo-logs"))
		if err != nil {
			return fmt.Errorf("connect nats %q: %w", natsURL, err)
		}
		defer conn.Close()

		logs, err := ingest.NewLogConsumer(repo.NewLogs(database.Pool()), defaultWorkers).Start(ctx, conn)
		if err != nil {
			return err
		}
		defer func() { _ = logs.Close() }()
		slog.Info("consuming logs", "subject", ingest.LogSubject, "nats", natsURL)

		traces, err := startTraces(ctx, database.Pool(), conn)
		if err != nil {
			return err
		}
		defer func() { _ = traces.Close() }()
		slog.Info("consuming traces", "subject", ingest.TraceSubject, "nats", natsURL)
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

// startTraces publishes a rate card and subscribes the trace consumer to it.
//
// The card is loaded from the database first and refreshed in the background
// afterwards, never the other way round: a process that waited on the published
// catalogue before consuming would price nothing while the feed was slow, and
// nothing at all for as long as it was down. Rates already stored answer both.
func startTraces(ctx context.Context, pool *pgxpool.Pool, conn *nats.Conn) (*ingest.Subscription, error) {
	prices := cost.NewRefresher(
		cost.NewStore(pool),
		cost.NewCatalogue(os.Getenv("LLM_PRICES_URL"), nil),
		cost.SourceHelicone,
		priceRefreshInterval(),
	)
	if err := prices.Load(ctx); err != nil {
		// Not fatal, because the failure is survivable and the alternative is not:
		// a call this service cannot price is stored as unpriced, which is honest
		// and fixable later, whereas refusing to consume would throw away the
		// trace itself over a number that sits beside it.
		slog.Error("could not load the stored rate card; model calls start unpriced", "error", err)
	}
	go prices.Run(ctx)

	consumer := ingest.NewTraceConsumer(
		repo.NewTraces(pool),
		ingest.NewIntegrationResolver(repo.NewDeployments(pool)),
		prices,
	)
	return consumer.Start(ctx, conn)
}

// priceRefreshInterval reads LLM_PRICES_REFRESH, falling back to the refresher's
// own default. A value that cannot be read is warned about and ignored: a typo in
// a tuning knob is no reason to stop a service from starting.
func priceRefreshInterval() time.Duration {
	raw := os.Getenv("LLM_PRICES_REFRESH")
	if raw == "" {
		return 0
	}
	interval, err := time.ParseDuration(raw)
	if err != nil {
		slog.Warn("invalid LLM_PRICES_REFRESH, using the default", "value", raw, "error", err)
		return 0
	}
	return interval
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
