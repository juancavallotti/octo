package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/juancavallotti/octo/sidecars/stats/internal/api"
	"github.com/juancavallotti/octo/sidecars/stats/internal/rollup"
	"github.com/juancavallotti/octo/sidecars/stats/internal/scrape"
	"github.com/juancavallotti/octo/sidecars/stats/internal/series"
	"github.com/juancavallotti/octo/sidecars/stats/internal/store"
)

// disabledLogEvery throttles the "runtime is not serving metrics" warning. That
// condition does not resolve itself, and at one scrape a second it would
// otherwise write 86,400 identical lines a day into the pod's log.
const disabledLogEvery = 5 * time.Minute

// sampler is the loop: scrape, encode, write the sample, and hand it to the
// collector, which returns a bucket whenever one closes.
//
// One goroutine owns the dictionary and the collector, which is what lets both
// be plain structs with no locking. Only the counters are shared, because the
// status endpoint reads them from the HTTP server's goroutines.
type sampler struct {
	cfg       config
	scraper   *scrape.Scraper
	dict      *series.Dictionary
	collector *rollup.Collector
	store     *store.Store
	counters  api.Counters

	// mu guards the fields the status endpoint reads that are not counters.
	// The dictionary and the collector themselves are NOT shared — the sampler
	// goroutine owns both — so what the status page needs from them is copied
	// out under this lock rather than read from them directly.
	mu         sync.Mutex
	generation int
	seriesLen  int
	openStart  int64
	openRows   int

	startedAt time.Time
	// lastDisabledLog throttles the metrics-disabled warning.
	lastDisabledLog time.Time
}

// newSampler wires a sampler from the config and an opened store.
func newSampler(cfg config, st *store.Store) *sampler {
	dict := series.NewDictionary()
	return &sampler{
		cfg:       cfg,
		scraper:   scrape.New(cfg.runtimeAdmin),
		dict:      dict,
		collector: rollup.NewCollector(cfg.rollup, dict),
		store:     st,
		startedAt: time.Now(),
	}
}

// Run samples until ctx is cancelled, then flushes the open bucket.
//
// The flush at the end is the reason this sidecar is a native sidecar rather
// than an ordinary container: Kubernetes terminates restartable init containers
// after the app containers, so by the time this returns the runtime has already
// stopped and the bucket being written is complete rather than truncated
// mid-shutdown.
func (s *sampler) Run(ctx context.Context) {
	// Meta first, so a reader can find the pod and learn its tier configuration
	// before any rows exist. Best effort: a Redis that is down at startup is a
	// condition this rides out, and the next successful write records it anyway.
	if err := s.store.WriteMeta(ctx, s.dict.Gen(), s.startedAt); err != nil {
		slog.Warn("could not record pod metadata", "error", err)
	}

	ticker := time.NewTicker(s.cfg.sample)
	defer ticker.Stop()

	// One sample immediately, so a pod that is about to be killed still leaves a
	// trace and so a misconfiguration surfaces in the first second rather than
	// after a full interval.
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			s.flush()
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick performs one scrape-encode-write cycle.
//
// Nothing in here is fatal. A failed scrape means this second has no sample; a
// failed write means it was not stored. Both are counted, reported on /status,
// and retried on the next tick — the sidecar's whole failure model is that it
// loses data rather than taking the pod down with it.
func (s *sampler) tick(ctx context.Context) {
	families, err := s.scraper.Scrape(ctx)
	if err != nil {
		s.counters.ScrapeFailed(err)
		s.logScrapeError(err)
		return
	}
	s.counters.Scraped()

	sample := s.dict.Encode(families, time.Now().UnixMilli())
	s.persistDictionary(ctx)

	if err := s.store.WriteSample(ctx, sample); err != nil {
		s.counters.WriteFailed(err)
		slog.Warn("could not write sample", "error", err)
	} else {
		s.counters.Wrote(time.Now())
	}

	if closed := s.collector.Add(sample); closed != nil {
		s.writeBucket(ctx, closed)
	}
	s.recordOpenBucket()
}

// recordOpenBucket copies the collector's progress somewhere the status
// endpoint can read it. The collector is not safe for concurrent use and is not
// made so for this: a status page is not worth putting a lock in the sampling
// path's way.
func (s *sampler) recordOpenBucket() {
	start, rows := s.collector.Open()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openStart, s.openRows = start, rows
}

// persistDictionary stores the current generation when the series set has
// grown since it was last written.
//
// Called between encoding a sample and writing it, so the dictionary a sample
// names is in Redis before the sample is — a reader never holds a row it cannot
// resolve. Encode has already advanced the generation if this scrape grew the
// dictionary; this only persists it.
//
// A failed write leaves the generation dirty and the next tick retries it. The
// samples written in between name that same generation and stay correct once it
// lands, because a dictionary is always written whole and indices are
// append-only, so every later generation is a superset of every earlier one.
func (s *sampler) persistDictionary(ctx context.Context) {
	if !s.dict.Dirty() {
		return
	}
	gen := s.dict.Gen()
	if err := s.store.WriteDictionary(ctx, gen, s.dict.Entries()); err != nil {
		s.counters.WriteFailed(err)
		slog.Warn("could not write series dictionary", "gen", gen, "error", err)
		return
	}
	s.dict.MarkClean()

	// Meta names the newest generation, so a reader can find it without parsing
	// a row first.
	if err := s.store.WriteMeta(ctx, gen, s.startedAt); err != nil {
		slog.Warn("could not update pod metadata", "error", err)
	}

	s.mu.Lock()
	s.generation, s.seriesLen = gen, s.dict.Len()
	s.mu.Unlock()

	slog.Info("series dictionary grew", "gen", gen, "series", s.dict.Len())
}

// writeBucket stores one collapsed bucket.
func (s *sampler) writeBucket(ctx context.Context, b *rollup.Bucket) {
	s.counters.BucketClosed()
	if err := s.store.WriteBucket(ctx, b); err != nil {
		s.counters.WriteFailed(err)
		slog.Warn("could not write rollup bucket", "start", b.StartMS, "error", err)
		return
	}
	slog.Info("rollup bucket closed",
		"start", time.UnixMilli(b.StartMS).UTC().Format(time.RFC3339),
		"samples", b.Samples, "series", len(b.Value))
}

// flush stores the bucket in progress on the way out.
//
// A fresh context, because the one that ended the loop is already cancelled and
// the whole point is to get this last write out during the termination grace
// period.
func (s *sampler) flush() {
	b := s.collector.Close()
	if b == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	s.writeBucket(ctx, b)
	s.recordOpenBucket()
}

// logScrapeError logs a failed scrape, throttling the one cause that will not
// resolve itself.
func (s *sampler) logScrapeError(err error) {
	if !errors.Is(err, scrape.ErrMetricsDisabled) {
		slog.Warn("could not scrape runtime metrics", "url", s.scraper.URL(), "error", err)
		return
	}
	if time.Since(s.lastDisabledLog) < disabledLogEvery {
		return
	}
	s.lastDisabledLog = time.Now()
	slog.Error("runtime is not serving metrics; nothing will be collected",
		"url", s.scraper.URL(),
		"fix", "start the runtime with --metrics or OCTO_METRICS=true")
}

// Report implements api.Reporter.
func (s *sampler) Report() api.Status {
	status := api.Status{
		Pod:            s.cfg.podName,
		DeploymentID:   s.cfg.deploymentID,
		MetricsURL:     s.scraper.URL(),
		SampleInterval: s.cfg.sample.String(),
		RollupInterval: s.cfg.rollup.String(),
		Retention:      s.cfg.retention.String(),
	}
	s.counters.Fill(&status)

	s.mu.Lock()
	status.Generation, status.Series = s.generation, s.seriesLen
	openStart, openRows := s.openStart, s.openRows
	s.mu.Unlock()

	if openRows > 0 {
		status.OpenBucketStart = time.UnixMilli(openStart).UTC().Format(time.RFC3339)
		status.OpenBucketRows = openRows
	}
	return status
}
