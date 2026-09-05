package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"
)

const (
	// defaultPort is this sidecar's own HTTP port. Deliberately not 8099, which
	// is the dev sidecar's: the two never share a pod, but a port that identifies
	// which sidecar answered is worth more than a number reused out of habit.
	defaultPort = "8098"
	// defaultRuntimeAdmin is the runtime's admin port on the shared loopback
	// (runtime/services/observability/observability.go:37).
	defaultRuntimeAdmin = "127.0.0.1:39999"

	// defaultSampleInterval is the live tier's resolution.
	defaultSampleInterval = time.Second
	// defaultRollupInterval is how wide a history bucket is. An hour of
	// one-second samples collapses to one row.
	defaultRollupInterval = time.Hour
	// defaultRetention is how far the history tier reaches back.
	defaultRetention = 7 * 24 * time.Hour

	// minSampleInterval floors the sample rate. Below this the sidecar spends
	// more time scraping and encoding than the runtime spends serving, and the
	// point of the exercise is to observe the pod rather than to load it.
	minSampleInterval = 100 * time.Millisecond
)

// config is everything the sidecar reads from its environment.
type config struct {
	port         string
	runtimeAdmin string
	redisURL     string
	deploymentID string
	podName      string

	sample    time.Duration
	rollup    time.Duration
	retention time.Duration
}

// loadConfig reads and validates the environment.
//
// Every problem is collected and reported together, so one restart reveals all
// of them rather than one per crash loop. Missing required values are a hard
// startup failure for the same reason the dev sidecar makes them one: a stats
// sidecar that does not know which deployment it belongs to, or has nowhere to
// write, has no job to do, and CrashLoopBackOff with a named cause is a better
// signal than a container that looks healthy and silently stores nothing.
//
// Note what is NOT a startup failure: a Redis that is configured but
// unreachable. That is a running condition this sidecar rides out, because
// failing on it would turn a cache outage into a restart storm across every
// production pod at once.
func loadConfig() (config, error) {
	cfg := config{
		port:         envOr("PORT", defaultPort),
		runtimeAdmin: envOr("RUNTIME_ADMIN_ADDR", defaultRuntimeAdmin),
		redisURL:     os.Getenv("REDIS_URL"),
		deploymentID: os.Getenv("DEPLOYMENT_ID"),
		podName:      os.Getenv("POD_NAME"),
	}

	var problems []string
	for _, req := range []struct{ name, value string }{
		{"REDIS_URL", cfg.redisURL},
		{"DEPLOYMENT_ID", cfg.deploymentID},
		{"POD_NAME", cfg.podName},
	} {
		if req.value == "" {
			problems = append(problems, "missing "+req.name)
		}
	}

	var err error
	if cfg.sample, err = durationOr("STATS_SAMPLE_INTERVAL", defaultSampleInterval); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.rollup, err = durationOr("STATS_ROLLUP_INTERVAL", defaultRollupInterval); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.retention, err = durationOr("STATS_RETENTION", defaultRetention); err != nil {
		problems = append(problems, err.Error())
	}
	problems = append(problems, cfg.checkIntervals()...)

	if len(problems) > 0 {
		return config{}, fmt.Errorf("invalid configuration: %v", problems)
	}
	return cfg, nil
}

// checkIntervals validates the three durations against each other.
//
// The two ratios ARE the tier depths — how many samples a live tier holds and
// how many buckets a retention window holds — and they are computed with integer
// division. So both ordering and divisibility are checked: 700ms samples in a 1s
// bucket orders fine and then truncates to a depth of one, which is a tier whose
// size nobody chose and no error anybody sees. Rejecting at startup is how an
// experiment with the intervals fails loudly rather than quietly measuring
// something else.
func (c config) checkIntervals() []string {
	var problems []string
	if c.sample > 0 && c.sample < minSampleInterval {
		problems = append(problems, fmt.Sprintf(
			"STATS_SAMPLE_INTERVAL %s is below the %s floor", c.sample, minSampleInterval))
	}
	if c.sample > 0 && c.rollup > 0 {
		switch {
		case c.rollup <= c.sample:
			problems = append(problems, fmt.Sprintf(
				"STATS_ROLLUP_INTERVAL %s must be longer than STATS_SAMPLE_INTERVAL %s", c.rollup, c.sample))
		case c.rollup%c.sample != 0:
			problems = append(problems, fmt.Sprintf(
				"STATS_ROLLUP_INTERVAL %s is not a whole number of STATS_SAMPLE_INTERVAL %s",
				c.rollup, c.sample))
		}
	}
	if c.rollup > 0 && c.retention > 0 {
		switch {
		case c.retention <= c.rollup:
			problems = append(problems, fmt.Sprintf(
				"STATS_RETENTION %s must be longer than STATS_ROLLUP_INTERVAL %s", c.retention, c.rollup))
		case c.retention%c.rollup != 0:
			problems = append(problems, fmt.Sprintf(
				"STATS_RETENTION %s is not a whole number of STATS_ROLLUP_INTERVAL %s",
				c.retention, c.rollup))
		}
	}
	return problems
}

// liveDepth is how many samples the live tier holds: one rollup bucket's worth.
func (c config) liveDepth() int64 { return int64(c.rollup / c.sample) }

// rollupDepth is how many collapsed buckets the history tier holds.
func (c config) rollupDepth() int64 { return int64(c.retention / c.rollup) }

// envOr returns the value of key, or fallback when it is empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// durationOr parses a Go duration from key, or returns fallback when it is
// empty. A value that does not parse, or is not positive, is an error rather
// than a silent fall back to the default: an operator who set the variable meant
// to change the behaviour, and quietly ignoring them is how an experiment
// produces a result about the wrong configuration.
func durationOr(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a duration", key, raw)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s %q must be positive", key, raw)
	}
	return d, nil
}

// parseLevel maps a LOG_LEVEL name to an slog.Level, defaulting to info. It
// matches the names the runtime and the other sidecars accept, so an operator
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
