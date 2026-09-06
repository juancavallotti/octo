// Command observability is the platform's observability service. It consumes
// what deployed runtimes ship over NATS as a competing consumer — log records on
// internal.logs, trace records on internal.traces — and persists both to Postgres
// for the platform to query. It also serves the pod stats the stats sidecar
// writes to Redis, the retention policy over what it stores, and the storage
// report on the two stores underneath all of it.
//
// It began as the log aggregator and was called "logs" for as long as that was
// the whole job. Traces, pod stats and retention landed here because each was the
// same shape of problem — records shipped in, history queried out — and the name
// followed once it described the thing.
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
	alertsource "github.com/juancavallotti/octo/observability/internal/alerting/source"
	alertstore "github.com/juancavallotti/octo/observability/internal/alerting/store"
	"github.com/juancavallotti/octo/observability/internal/api"
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

	// How a run of near-identical trace records is collapsed into one.
	//
	// foldWindow is how long a run stays open with nothing arriving, and it is the
	// only thing that ends a run that simply stopped. A second is comfortably longer
	// than the gap between two frames of a stream — those arrive tens of times a
	// second — and short enough that a block record in an ordinary flow, which folds
	// nothing, is stored about as promptly as it was before. That delay is the price
	// of folding at all, and it is affordable only because traces are read after the
	// fact rather than watched live.
	foldWindow = time.Second
	// A backstop for nothing ever sweeping again — a replica that died holding open
	// runs — rather than a second deadline. Well above the window so it never
	// competes with it.
	foldTTL = 10 * time.Minute
	// The cap on a run's merged text. Generous, because the point of merging is to
	// read a streamed answer back as prose and an answer cut off at the interesting
	// part would leave the row honest and useless. Past it the fold is marked
	// truncated — the same flag the runtime sets when it drops a payload of its own.
	foldMaxBodyBytes = 32 * 1024
	// The shortest run worth rewriting. Below this the attributes a fold adds cost
	// more than the rows it saves, and a reader learns nothing from being told that
	// a two-record span is two records.
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

	// Redis is the one dependency this service refuses to start without, and it is
	// deliberately not treated like the two below it.
	//
	// A missing DATABASE_URL or NATS_URL degrades to "serving /healthz while the
	// dependencies come up", which is right for both: they are reachable or they
	// are not, and the consumers reconnect. Redis is different because what it
	// holds is not a connection but a decision — the fold that collapses a
	// streaming block's per-frame trace records into one row. An aggregator that
	// started without it would look healthy, consume normally, and quietly store
	// tens of thousands of rows per conversation again, which is the bug the fold
	// exists to fix and is invisible until the table is large.
	//
	// So this fails loudly, at the one moment somebody is watching, naming the
	// variable and the chart value that sets it.
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

	// Alerting, which needs the database and nothing else to be useful: a watch
	// with a log action works with no broker and no orchestrator, and one with a
	// topic or email action records that it could not deliver rather than
	// stopping the service from starting.
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

// startAlerting starts the watch evaluator.
//
// Wired here, beside the consumers, and gated on the database for the same
// reason they are: a watch is a question about tables this process may not have.
// The leader election is what makes it safe to run on every replica — ingesting a
// record twice is idempotent, and evaluating a watch twice is not.
//
// A failure to build the elector is fatal. It means this pod has a cluster
// identity and cannot reach the API server, and a service that shrugged and
// elected itself would put two evaluators on one installation.
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
	)
	go runner.Run(ctx)
	slog.Info("evaluating alerting watches", "identity", elector.Identity())
	return alerting.NewService(store, runner, alertaction.Validate), nil
}

// startTraces publishes a rate card and subscribes the trace consumer to it.
//
// The card is loaded from the database first and refreshed in the background
// afterwards, never the other way round: a process that waited on the published
// catalogue before consuming would price nothing while the feed was slow, and
// nothing at all for as long as it was down. Rates already stored answer both.
func startTraces(ctx context.Context, pool *pgxpool.Pool, conn *nats.Conn, rdb *redis.Client) (*ingest.Subscription, error) {
	store := cost.NewStore(pool)
	interval := priceRefreshInterval()

	var sources []*cost.Refresher
	for _, source := range priceSources() {
		refresher := cost.NewRefresher(store, catalogueFor(source), source, interval)
		if err := refresher.Load(ctx); err != nil {
			// Not fatal, because the failure is survivable and the alternative is
			// not: a call this service cannot price is stored as unpriced, which
			// is honest and fixable later, whereas refusing to consume would
			// throw away the trace itself over a number that sits beside it.
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
// order. An unknown name is warned about and skipped rather than fatal, on the
// same terms as an unparseable refresh interval: a typo in a tuning knob is no
// reason to stop a service from starting. A value naming nothing usable falls
// back to the default, because pricing nothing at all is never what was meant.
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

// catalogue is the part of a published rate card this file uses. It is declared
// here, where it is consumed, because the two readers are different types and
// the only thing this needs from either is the fetch.
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
// liveness probe passes and the description reads even before Postgres is
// reachable.
//
// Redis is passed separately from the database because the two are not
// optional in the same way. This service refuses to start without a Redis and
// degrades to serving /healthz without a Postgres, so anything backed by Redis
// registers unconditionally while anything backed by Postgres cannot.
func newServer(database *db.DB, rdb *redis.Client, alerts *alerting.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	openapi.NewHandler().Register(mux)
	slog.Info("openapi routes registered",
		"endpoints", "GET /openapi.json, GET /openapi/operations")

	// Outside the database check, unlike everything below it: pod stats live in
	// Redis, which this service refuses to start without. Gating them on a
	// Postgres they do not use would take them away for the one failure that
	// cannot affect them.
	api.NewStatsHandler(podstats.NewService(podstats.NewReader(rdb))).Register(mux)
	slog.Info("pod stats API registered", "endpoints",
		"GET /stats/{deploymentId}/pods, GET /stats/{deploymentId}/metrics, "+
			"GET /stats/{deploymentId}/series")

	// The storage report is outside the gate for the same reason, and it takes the
	// pool as possibly nil on purpose: the half of the report about a store this
	// process does not have is a reason rather than a failure, and the Redis half
	// is worth having while Postgres is still coming up.
	api.NewStorageHandler(storagestats.NewService(rdb, databasePool(database))).Register(mux)
	slog.Info("storage report registered", "endpoints", "GET /settings/storage")

	if database != nil {
		api.NewLogsHandler(repo.NewLogs(database.Pool())).Register(mux)
		api.NewTracesHandler(repo.NewTraces(database.Pool())).Register(mux)
		slog.Info("query API registered", "endpoints", "GET /logs, GET /traces, "+
			"GET /traces/apps, GET /traces/{traceId}, GET /traces/{traceId}/records/{id}")

		// Data retention: the policy for how long the two streams above are kept,
		// and the sweep that enforces it. It belongs to this service because this
		// service owns the tables a sweep deletes from — the policy itself is a
		// row in site_settings, so it costs a key rather than a migration.
		api.NewRetentionHandler(retention.NewService(database.Pool())).Register(mux)
		slog.Info("retention routes registered",
			"endpoints", "GET/PUT /settings/retention, POST /retention/run")

		// Alerting. The service is registered whether or not this replica is the
		// one evaluating: reading and editing a watch is not leader work, and a
		// standby replica answering the API is what makes the platform's requests
		// land anywhere rather than on one pod.
		api.NewAlertsHandler(alerts).Register(mux)
		slog.Info("alerting routes registered", "endpoints",
			"GET/POST /alerts/watches, GET/PUT/DELETE /alerts/watches/{id}, "+
				"POST /alerts/watches/{id}/mute, GET /alerts/watches/{id}/evaluations, "+
				"POST /alerts/preview, GET /alerts/evaluations, GET /alerts/incidents, "+
				"POST /alerts/incidents/{id}/ack")
	}
	return mux
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
// A named function rather than the closure it used to be, because an annotation
// has to hang off something the generator can see.
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
