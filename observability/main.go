// Command observability consumes telemetry over NATS as a competing consumer —
// log records on internal.logs, trace records on internal.traces — and persists
// both to Postgres. It serves that history back, along with the pod stats it reads
// from Redis, the retention policy over what it keeps, and a report on how full
// the two stores underneath are.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/juancavallotti/octo/observability/internal/alerting"
	alertaction "github.com/juancavallotti/octo/observability/internal/alerting/action"
	alertcooldown "github.com/juancavallotti/octo/observability/internal/alerting/cooldown"
	alertsource "github.com/juancavallotti/octo/observability/internal/alerting/source"
	alertstore "github.com/juancavallotti/octo/observability/internal/alerting/store"
	"github.com/juancavallotti/octo/observability/internal/api"
	"github.com/juancavallotti/octo/observability/internal/authz"
	"github.com/juancavallotti/octo/observability/internal/cost"
	"github.com/juancavallotti/octo/observability/internal/db"
	"github.com/juancavallotti/octo/observability/internal/fold"
	"github.com/juancavallotti/octo/observability/internal/ingest"
	"github.com/juancavallotti/octo/observability/internal/leader"
	"github.com/juancavallotti/octo/observability/internal/openapi"
	"github.com/juancavallotti/octo/observability/internal/podstats"
	"github.com/juancavallotti/octo/observability/internal/redisx"
	"github.com/juancavallotti/octo/observability/internal/repo"
	"github.com/juancavallotti/octo/observability/internal/retention"
	"github.com/juancavallotti/octo/observability/internal/storagestats"
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
	// The whole request, headers and body. Generous next to the header deadline
	// because an ingest POST carries a batch, and still finite: without it a client
	// can drip a body for as long as it likes and hold the goroutine reading it.
	readTimeout = 60 * time.Second

	// How a run of near-identical trace records is collapsed into one.
	//
	// foldWindow is how long a run stays open with nothing arriving, and it is the
	// only thing that ends a run that simply stopped. A second is comfortably longer
	// than the gap between two frames of a stream, and short enough that a record
	// which folds nothing is still stored promptly. That delay is the price of
	// folding at all, affordable because traces are read after the fact rather than
	// watched live.
	foldWindow = time.Second
	// A backstop for nothing ever sweeping again — a replica that died holding open
	// runs — rather than a second deadline. Well above the window so it never
	// competes with it.
	foldTTL = 10 * time.Minute
	// The cap on a run's merged text. Generous, because the point of merging is to
	// read a streamed answer back as prose and an answer cut off at the interesting
	// part would be honest and useless. Past it the fold is marked truncated.
	foldMaxBodyBytes = 32 * 1024
	// The shortest run worth rewriting. Below this the attributes a fold adds cost
	// more than the rows it saves.
	foldMinRun = 4
)

func main() {
	level, levelErr := parseLevel(os.Getenv("LOG_LEVEL"))
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	if levelErr != nil {
		slog.Warn("invalid LOG_LEVEL, defaulting to info", "error", levelErr)
	}

	if err := run(); err != nil {
		slog.Error("observability service stopped with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	port := envOr("PORT", defaultPort)
	dsn := os.Getenv("DATABASE_URL")

	// Root context cancelled on SIGINT/SIGTERM so pod termination drains cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Held across the consumer block below so alerting can publish on the same
	// connection. Nil when this process has no broker, which is a service that
	// cannot deliver a topic action and can still evaluate every watch.
	var natsConn *nats.Conn

	// Redis is the one dependency this service refuses to start without. A missing
	// DATABASE_URL or NATS_URL degrades to serving /healthz until they are
	// reachable, but Redis holds the fold that collapses a streaming block's
	// per-frame trace records into one row. Starting without it would look healthy
	// and quietly store tens of thousands of rows per conversation, which is
	// invisible until the table is large — so it fails loudly instead.
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return errors.New("REDIS_URL is not set: the aggregator folds trace records in " +
			"Redis and will not run without one — set redis.enabled=true, or point " +
			"externalRedis.url at a Redis this cluster can reach")
	}
	rdb, err := redisx.Open(ctx, redisURL)
	if err != nil {
		return err
	}
	defer func() { _ = rdb.Close() }()
	slog.Info("connected to redis")

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
		conn, err := nats.Connect(natsURL, nats.Name("octo-observability"))
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

		traces, err := startTraces(ctx, database.Pool(), conn, rdb)
		if err != nil {
			return err
		}
		defer func() { _ = traces.Close() }()
		slog.Info("consuming traces", "subject", ingest.TraceSubject, "nats", natsURL)

		natsConn = conn
	}

	// Alerting needs the database and nothing else to be useful: a watch with a log
	// action works with no broker, and one with a topic or email action records
	// that it could not deliver rather than stopping the service from starting.
	var alerts *alerting.Service
	if database != nil {
		var err error
		if alerts, err = startAlerting(ctx, database.Pool(), rdb, natsConn); err != nil {
			return err
		}
	}

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           newServer(database, rdb, alerts),
		ReadHeaderTimeout: readHeaderTimeout,
		// The header deadline ends when the headers do, and this service serves
		// POSTs and PUTs with bodies. Without a deadline on the whole read, a
		// client can drip a body indefinitely and hold a handler goroutine and a
		// connection for as long as it likes.
		ReadTimeout: readTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("observability service listening", "addr", httpServer.Addr, "db", database != nil)
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

// startAlerting starts the watch evaluator. It is gated on the database because a
// watch is a question about tables this process may not have, and the leader
// election is what makes it safe to run on every replica — ingesting a record
// twice is idempotent, evaluating a watch twice is not.
//
// A failure to build the elector is fatal: a service that shrugged and elected
// itself would put two evaluators on one installation.
func startAlerting(
	ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, conn *nats.Conn,
) (*alerting.Service, error) {
	elector, err := leader.New(ctx)
	if err != nil {
		return nil, err
	}

	dispatcher := alertaction.NewDispatcher(
		alertaction.NewTopics(conn),
		alertaction.NewMailer(os.Getenv("ORCHESTRATOR_URL")),
	)
	store := alertstore.New(pool)
	runner := alerting.NewRunner(
		store,
		alertsource.New(pool, podstats.NewService(podstats.NewReader(rdb))),
		elector,
		dispatcher,
		// The cooldown record is the one piece of alerting state in Redis because it
		// is disposable: losing it means somebody is told twice, where losing what is
		// in Postgres would restart a hold or re-announce an incident.
		alertcooldown.New(rdb),
	)
	go runner.Run(ctx)
	slog.Info("evaluating alerting watches", "identity", elector.Identity())
	return alerting.NewService(store, runner, alertaction.Validate), nil
}

// startTraces publishes a rate card and subscribes the trace consumer to it.
//
// The card is loaded from the database first and refreshed in the background
// afterwards, never the other way round: waiting on the published catalogue before
// consuming would price nothing while the feed was slow, and nothing at all for as
// long as it was down.
func startTraces(ctx context.Context, pool *pgxpool.Pool, conn *nats.Conn, rdb *redis.Client) (*ingest.Subscription, error) {
	store := cost.NewStore(pool)
	interval := priceRefreshInterval()

	var sources []*cost.Refresher
	for _, source := range priceSources() {
		refresher := cost.NewRefresher(store, catalogueFor(source), source, interval)
		if err := refresher.Load(ctx); err != nil {
			// Not fatal: a call this service cannot price is stored as unpriced,
			// which is fixable later, whereas refusing to consume would throw away
			// the trace itself over a number that sits beside it.
			slog.Error("could not load a stored rate card; it starts empty",
				"source", source, "error", err)
		}
		go refresher.Run(ctx)
		sources = append(sources, refresher)
	}
	slog.Info("pricing model calls", "sources", priceSources())

	consumer := ingest.NewTraceConsumer(
		repo.NewTraces(pool),
		ingest.NewIntegrationResolver(repo.NewDeployments(pool)),
		cost.NewPricer(sources...),
		fold.NewStore(rdb, foldWindow, foldTTL, foldMaxBodyBytes, foldMinRun),
	)
	return consumer.Start(ctx, conn)
}

// defaultPriceSources is the order a card is looked up in, most preferred first.
//
// OpenRouter leads because its card is priced per model by the platform that
// sells them and turns over daily; helicone follows because it carries the
// patterns OpenRouter never publishes — Bedrock, Azure, vendor-hosted ids. A
// model either card knows is priced; only one neither knows is not.
var defaultPriceSources = []string{cost.SourceOpenRouter, cost.SourceHelicone}

// priceSources reads LLM_PRICES_SOURCES, a comma-separated list in preference
// order. An unknown name is warned about and skipped rather than fatal: a typo in
// a tuning knob is no reason to stop a service from starting. A value naming
// nothing usable falls back to the default.
func priceSources() []string {
	raw := os.Getenv("LLM_PRICES_SOURCES")
	if strings.TrimSpace(raw) == "" {
		return defaultPriceSources
	}

	var sources []string
	for _, name := range strings.Split(raw, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		switch name {
		case "":
		case cost.SourceOpenRouter, cost.SourceHelicone:
			sources = append(sources, name)
		default:
			slog.Warn("unknown LLM_PRICES_SOURCES entry, skipping it",
				"source", name, "known", defaultPriceSources)
		}
	}
	if len(sources) == 0 {
		slog.Warn("LLM_PRICES_SOURCES named no known source, using the default",
			"value", raw, "default", defaultPriceSources)
		return defaultPriceSources
	}
	return sources
}

// catalogue is the part of a published rate card this file uses: the two readers
// are different types and the fetch is all either is needed for.
type catalogue interface {
	Fetch(ctx context.Context) (cost.Fetched, error)
}

// catalogueFor builds the reader for one source. Each takes its own URL
// override, so a cluster without egress can mirror one, the other, or both.
func catalogueFor(source string) catalogue {
	if source == cost.SourceOpenRouter {
		return cost.NewOpenRouterCatalogue(os.Getenv("LLM_PRICES_OPENROUTER_URL"), nil)
	}
	return cost.NewCatalogue(os.Getenv("LLM_PRICES_URL"), nil)
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

// newServer wires the HTTP routes. The query API is registered only when a
// database is configured; /healthz and the API description always serve, so a
// liveness probe passes even before Postgres is reachable.
//
// Redis is passed separately because the two stores are not optional in the same
// way: anything backed by Redis registers unconditionally, since the service
// refuses to start without one.
func newServer(database *db.DB, rdb *redis.Client, alerts *alerting.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	openapi.NewHandler().Register(mux)
	slog.Info("openapi routes registered",
		"endpoints", "GET /openapi.json, GET /openapi/operations")

	// Outside the database check: pod stats live in Redis, and gating them on a
	// Postgres they do not use would take them away for a failure that cannot
	// affect them.
	api.NewStatsHandler(podstats.NewService(podstats.NewReader(rdb))).Register(mux)
	slog.Info("pod stats API registered", "endpoints",
		"GET /stats/{deploymentId}/pods, GET /stats/{deploymentId}/metrics, "+
			"GET /stats/{deploymentId}/series")

	// The storage report takes the pool as possibly nil: the half of the report
	// about a store this process does not have is a reason rather than a failure,
	// and the Redis half is worth having while Postgres is still coming up.
	api.NewStorageHandler(storagestats.NewService(rdb, databasePool(database))).Register(mux)
	slog.Info("storage report registered", "endpoints", "GET /settings/storage")

	if database != nil {
		api.NewLogsHandler(repo.NewLogs(database.Pool())).Register(mux)
		api.NewTracesHandler(repo.NewTraces(database.Pool())).Register(mux)
		slog.Info("query API registered", "endpoints", "GET /logs, GET /traces, "+
			"GET /traces/apps, GET /traces/{traceId}, GET /traces/{traceId}/records/{id}")

		// Data retention: the policy for how long the two streams above are kept, and
		// the sweep that enforces it. The policy is a row in site_settings, so it
		// costs a key rather than a migration.
		api.NewRetentionHandler(retention.NewService(database.Pool())).Register(mux)
		slog.Info("retention routes registered",
			"endpoints", "GET/PUT /settings/retention, POST /retention/run")

		// Alerting. Registered whether or not this replica is the one evaluating:
		// reading and editing a watch is not leader work, so a request lands on any
		// replica rather than on one.
		api.NewAlertsHandler(alerts).Register(mux)
		slog.Info("alerting routes registered", "endpoints",
			"GET/POST /alerts/watches, GET/PUT/DELETE /alerts/watches/{id}, "+
				"POST /alerts/watches/{id}/mute, GET /alerts/watches/{id}/evaluations, "+
				"POST /alerts/preview, GET /alerts/evaluations, GET /alerts/incidents, "+
				"POST /alerts/incidents/{id}/ack")
	}
	return guard(mux)
}

// guard wraps the API in the authorization policy, when IAM_URL names an issuer
// to verify tokens against. Without one it returns the mux untouched and says so:
// a service that cannot verify a token must not start refusing every request,
// because there is no way for a caller to fix that from the outside.
func guard(mux http.Handler) http.Handler {
	issuer := os.Getenv("IAM_URL")
	if issuer == "" {
		slog.Warn("IAM_URL is unset, so the API authorizes nothing and serves every caller")
		return mux
	}
	slog.Info("api authorization enabled", "iam", issuer)
	return authz.Wrap(authz.NewVerifier(issuer), mux)
}

// databasePool returns the connection pool, or nil when this process is running
// without a database.
func databasePool(database *db.DB) *pgxpool.Pool {
	if database == nil {
		return nil
	}
	return database.Pool()
}

// healthz reports that the process is up.
//
//	@Summary		Liveness
//	@Description	Answers as soon as the process is serving, whether or not Postgres or
//	@Description	NATS are reachable — it is what the readiness probe polls while the
//	@Description	rest is still coming up.
//	@Tags			meta
//	@Produce		plain
//	@Success		200	{string}	string	"ok"
//	@Router			/healthz [get]
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

// envOr returns the value of key, or fallback when it is empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseLevel maps a LOG_LEVEL name to an slog.Level, defaulting to info.
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
