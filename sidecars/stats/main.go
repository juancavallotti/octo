// Command stats-sidecar gives one production integration pod a rolling week of
// its own telemetry.
//
// It runs beside the runtime container, samples that container's Prometheus
// endpoint over loopback once a second, and writes two tiers to Redis: the live
// tier at full resolution for the bucket in progress, and a history tier where
// each completed bucket is one collapsed row. A week of one-second samples is
// 604,800 rows a pod and a week of hourly rows is 168, which is the whole reason
// the second tier exists.
//
// Why this exists. A deployed integration pod is observable only while somebody
// is watching it: the runtime can serve metrics, nothing scrapes them, and when
// a deployment misbehaved an hour ago there is no record of what it was doing.
// This is the record. It is write-only for now — nothing reads these keys yet —
// but the layout is deployment-first so that one deployment id finds every pod
// that ever reported, which is what the monitoring feature this precedes needs.
//
// # What it is not allowed to do
//
// It must never be able to take a production pod out of service. It is injected
// as a native sidecar, and Kubernetes folds a restartable init container's
// readiness into the pod's, so a truthful readiness probe here would let a Redis
// outage stop traffic to every integration in the namespace at once. Both probes
// therefore answer 200 whenever the process is running, the orchestrator
// attaches no readiness probe at all, and every failure — an unreachable Redis,
// a runtime that is not serving metrics — is counted and logged rather than
// signalled. Losing statistics is always the right trade against losing traffic.
//
// It holds no Kubernetes credential and never touches the cluster API, the same
// invariant the dev sidecar keeps. It learns which pod it is from the downward
// API and which deployment from its environment, and speaks to exactly two
// peers: the runtime on loopback, and Redis.
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

	"github.com/redis/go-redis/v9"

	"github.com/juancavallotti/octo/sidecars/stats/internal/api"
	"github.com/juancavallotti/octo/sidecars/stats/internal/redisx"
	"github.com/juancavallotti/octo/sidecars/stats/internal/store"
)

const (
	// shutdownTimeout bounds how long in-flight requests have to drain on
	// SIGTERM. Everything served here is small and immediate.
	shutdownTimeout = 5 * time.Second
	// flushTimeout bounds the last write on the way out. The sampler gets it
	// after the HTTP server has been told to stop, and it is the reason this is a
	// native sidecar: the runtime has already terminated by then, so the bucket
	// being flushed is complete rather than truncated.
	flushTimeout = 5 * time.Second

	// HTTP server timeouts. The endpoints are unauthenticated, so a client with
	// network reach could otherwise pin a goroutine mid-header, mid-body or on an
	// idle keep-alive. Everything here answers in microseconds, so all four are
	// tight — unlike the dev sidecar, which needs a generous write timeout for
	// its multi-megabyte /metrics passthrough.
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second

	// Redis client tuning, applied on top of whatever the URL parsed to.
	//
	// The defaults are built for a caller that needs its command to succeed and
	// will wait to make that happen: three retries with backoff, on top of the
	// connection pool's own dial attempts, which is several seconds before a
	// down server is reported. That is the wrong shape here. Samples are taken
	// once a second and each is written as it is taken, so a write that spends
	// five seconds retrying does not rescue that sample — it swallows the four
	// after it, and stalls the loop that would otherwise have kept scraping.
	//
	// Failing fast is what keeps the sidecar responsive through an outage: the
	// sample is lost either way, and the next tick gets a clean attempt.
	redisMaxRetries  = 1
	redisDialTimeout = 2 * time.Second
	redisOpTimeout   = 2 * time.Second
)

func main() {
	level, levelErr := parseLevel(os.Getenv("LOG_LEVEL"))
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	if levelErr != nil {
		slog.Warn("invalid LOG_LEVEL, defaulting to info", "error", levelErr)
	}

	if err := run(); err != nil {
		slog.Error("stats sidecar stopped with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// New rather than Open: the lazy client, with no PING to prove it. A Redis
	// that is down at startup is a running condition this rides out, not a reason
	// to fail — see the note in internal/redisx. Only a URL that does not parse
	// stops the process, because that is a mistake no amount of waiting fixes.
	client, err := openRedis(cfg.redisURL)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	sampler := newSampler(cfg, store.New(client, store.Config{
		DeploymentID:   cfg.deploymentID,
		PodName:        cfg.podName,
		SampleInterval: cfg.sample,
		RollupInterval: cfg.rollup,
		Retention:      cfg.retention,
		LiveDepth:      cfg.liveDepth(),
		RollupDepth:    cfg.rollupDepth(),
	}))

	mux := http.NewServeMux()
	api.NewHandler(sampler).Register(mux)
	httpServer := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("stats sidecar listening",
			"addr", httpServer.Addr,
			"deployment", cfg.deploymentID,
			"pod", cfg.podName,
			"runtimeAdmin", cfg.runtimeAdmin,
			"sample", cfg.sample,
			"rollup", cfg.rollup,
			"retention", cfg.retention,
			"liveDepth", cfg.liveDepth(),
			"rollupDepth", cfg.rollupDepth(),
			"keys", store.Layout,
			"endpoints", api.Endpoints(),
		)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// The sampler owns the rest of the process's life. It returns only when the
	// context is cancelled, having flushed the bucket in progress.
	done := make(chan struct{})
	go func() {
		defer close(done)
		sampler.Run(ctx)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining")
	}

	// The server first, so nothing new arrives, then the sampler's final flush.
	shutdownErr := shutdown(httpServer, shutdownTimeout)
	<-done
	return shutdownErr
}

// openRedis validates the URL through redisx and returns a client tuned for
// sampling.
//
// redisx owns two things worth not reimplementing: how the URL is parsed and
// how a parse failure is reported without the password in it. The retry policy
// is deliberately not one of them — it is the caller's to choose, and this
// caller wants failure reported rather than retried. See the constants above.
//
// The tuned client is built from a FRESH ParseURL rather than from a copy of
// the validated client's Options. A go-redis Options carries internal
// registration state, and reusing one makes the second client log a spurious
// "cannot overwrite existing handler" error at every startup.
func openRedis(url string) (*redis.Client, error) {
	validated, err := redisx.New(url)
	if err != nil {
		return nil, err
	}
	_ = validated.Close()

	// Cannot fail: redisx.New just parsed the same string.
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis: %w", err)
	}
	opts.MaxRetries = redisMaxRetries
	opts.DialTimeout = redisDialTimeout
	opts.ReadTimeout = redisOpTimeout
	opts.WriteTimeout = redisOpTimeout
	return redis.NewClient(opts), nil
}

// shutdown drains the server within the given grace period, on a fresh context:
// the one that triggered this is already cancelled.
func shutdown(server *http.Server, grace time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err //nolint:wrapcheck // the only caller logs it as the process's exit cause
	}
	return nil
}
