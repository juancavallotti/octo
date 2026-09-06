package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/juancavallotti/octo/logs/internal/httpx"
	"github.com/juancavallotti/octo/logs/internal/podstats"
)

const (
	// statsDefaultPoints and statsMaxPoints bound one series. Named apart from
	// defaultLimit/maxLimit in logs.go, which are row counts for a different
	// API in the same package and should not quietly become shared.
	statsDefaultPoints = 1000
	statsMaxPoints     = 5000

	// statsMaxMetrics caps how many names one query may ask for. A name is not
	// a series — a histogram expands to one per bucket boundary per flow — so
	// this is the coarse bound and MaxSelectedSeries is the real one.
	statsMaxMetrics = 20

	// statsDefaultWindow is what a query with no bounds reads. Short, because
	// the intent of this data is watching something now.
	statsDefaultWindow = 15 * time.Minute
)

// StatsReader is the deployment-scoped view of stored pod stats. The handler
// depends on the interface so it can be tested without a Redis.
type StatsReader interface {
	Pods(ctx context.Context, deploymentID string) ([]podstats.PodStatus, bool, error)
	Metrics(ctx context.Context, deploymentID string, pods []string, prefix string) ([]podstats.Metric, []podstats.Warning, error)
	Series(ctx context.Context, q podstats.Query) (podstats.Result, error)
}

// StatsHandler serves the pod stats query API.
type StatsHandler struct {
	r StatsReader
	// now is the clock the default window is measured from, replaced in tests.
	now func() time.Time
}

// NewStatsHandler returns a handler backed by r.
func NewStatsHandler(r StatsReader) *StatsHandler {
	return &StatsHandler{r: r, now: time.Now}
}

// Register wires the routes onto mux.
func (h *StatsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /stats/{deploymentId}/pods", h.pods)
	mux.HandleFunc("GET /stats/{deploymentId}/metrics", h.metrics)
	mux.HandleFunc("GET /stats/{deploymentId}/series", h.series)
}

// statsPod is one pod's reporting state and configuration.
type statsPod struct {
	Pod       string    `json:"pod"`
	LastSeen  time.Time `json:"lastSeen"`
	Reporting bool      `json:"reporting"`
	StartedAt *string   `json:"startedAt,omitempty"`

	SampleInterval string `json:"sampleInterval"`
	RollupInterval string `json:"rollupInterval"`
	Retention      string `json:"retention"`

	Generation int   `json:"generation"`
	Series     int   `json:"series"`
	LiveRows   int64 `json:"liveRows"`
	RollupRows int64 `json:"rollupRows"`
}

type statsPodsResponse struct {
	DeploymentID string     `json:"deploymentId"`
	Items        []statsPod `json:"items"`
	Truncated    bool       `json:"truncated"`
}

// pods lists the pods of a deployment that have reported.
//
//	@Summary		List the pods reporting stats for a deployment
//	@Description	Every pod of the deployment the stats index still holds, most recently seen
//	@Description	first. Read this first: it names the pods the other two routes accept, and
//	@Description	says whether each is still reporting.
//	@Description
//	@Description	liveRows and rollupRows are reported separately on purpose. Zero live rows
//	@Description	beside a full history is the ordinary state of a pod that stopped a few hours
//	@Description	ago, because the live tier is kept for only twice the rollup interval while
//	@Description	the pod stays in the index for the whole retention window.
//	@Description
//	@Description	An unknown deployment answers 200 with no items rather than 404. This service
//	@Description	holds no deployment registry, so it cannot tell a deployment that never
//	@Description	existed from one whose sidecar is switched off or whose stats have expired.
//	@Tags			stats
//	@Produce		json
//	@Param			deploymentId	path		string	true	"The deployment to describe"
//	@Success		200				{object}	statsPodsResponse
//	@Failure		500				{object}	httpx.ErrorResponse
//	@Router			/stats/{deploymentId}/pods [get]
func (h *StatsHandler) pods(w http.ResponseWriter, r *http.Request) {
	deploymentID := r.PathValue("deploymentId")

	pods, truncated, err := h.r.Pods(r.Context(), deploymentID)
	if err != nil {
		slog.Error("api: list stats pods", "deployment", deploymentID, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to read pod stats")
		return
	}

	items := make([]statsPod, 0, len(pods))
	for _, p := range pods {
		item := statsPod{
			Pod:            p.Pod,
			LastSeen:       p.LastSeen.UTC(),
			Reporting:      p.Reporting,
			SampleInterval: p.Meta.SampleInterval.String(),
			RollupInterval: p.Meta.RollupInterval.String(),
			Retention:      p.Meta.Retention.String(),
			Generation:     p.Meta.Gen,
			Series:         p.Series,
			LiveRows:       p.LiveRows,
			RollupRows:     p.RollupRows,
		}
		if !p.Meta.StartedAt.IsZero() {
			started := p.Meta.StartedAt.UTC().Format(time.RFC3339)
			item.StartedAt = &started
		}
		items = append(items, item)
	}

	httpx.WriteJSON(w, http.StatusOK, statsPodsResponse{
		DeploymentID: deploymentID,
		Items:        items,
		Truncated:    truncated,
	})
}

// statsMetricSeries is one label set of a metric and the pods exposing it.
type statsMetricSeries struct {
	Labels map[string]string `json:"labels,omitempty"`
	Pods   []string          `json:"pods"`
}

// statsMetric groups a metric's label sets under its name, so a histogram reads
// as one metric with a series per boundary rather than as many unrelated names.
type statsMetric struct {
	Name   string              `json:"name"`
	Kind   string              `json:"kind"`
	Series []statsMetricSeries `json:"series"`
}

type statsWarning struct {
	Pod    string `json:"pod"`
	Reason string `json:"reason"`
}

type statsMetricsResponse struct {
	DeploymentID string         `json:"deploymentId"`
	Items        []statsMetric  `json:"items"`
	Warnings     []statsWarning `json:"warnings"`
}

// metrics lists the series a deployment's pods expose, reading no rows.
//
//	@Summary		List the metrics a deployment exposes
//	@Description	The catalogue, built from the pods' dictionaries alone — no samples are read.
//	@Description	It exists so a caller can find exact metric names before asking for data,
//	@Description	which is what makes the required metric filter on the series route usable.
//	@Description
//	@Description	Label sets are nested under their metric name. One histogram is a single
//	@Description	entry with a series per bucket boundary, rather than a hundred names that
//	@Description	happen to share a prefix.
//	@Tags			stats
//	@Produce		json
//	@Param			deploymentId	path		string		true	"The deployment to describe"
//	@Param			pod				query		[]string	false	"Only these pods; repeat the parameter for more than one"	collectionFormat(multi)
//	@Param			prefix			query		string		false	"Only metrics whose name starts with this"
//	@Success		200				{object}	statsMetricsResponse
//	@Failure		500				{object}	httpx.ErrorResponse
//	@Router			/stats/{deploymentId}/metrics [get]
func (h *StatsHandler) metrics(w http.ResponseWriter, r *http.Request) {
	deploymentID := r.PathValue("deploymentId")
	query := r.URL.Query()

	metrics, warnings, err := h.r.Metrics(r.Context(), deploymentID,
		query["pod"], query.Get("prefix"))
	if err != nil {
		slog.Error("api: list stats metrics", "deployment", deploymentID, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to read pod stats")
		return
	}

	items := make([]statsMetric, 0, len(metrics))
	for _, m := range metrics {
		item := statsMetric{Name: m.Name, Kind: m.Kind.String()}
		for _, s := range m.Series {
			item.Series = append(item.Series, statsMetricSeries{Labels: s.Labels, Pods: s.Pods})
		}
		items = append(items, item)
	}

	httpx.WriteJSON(w, http.StatusOK, statsMetricsResponse{
		DeploymentID: deploymentID,
		Items:        items,
		Warnings:     encodeWarnings(warnings),
	})
}

// statsSeries is one decoded series, columnar and oldest-first.
//
// Columnar because a series is thousands of points and every repeated key is
// paid per point. Times are unix milliseconds for the same reason — a thousand
// RFC3339 strings is mostly punctuation, for a number every chart converts
// straight back to an integer. The window in the envelope stays RFC3339, and so
// do the parameters.
type statsSeries struct {
	Pod    string            `json:"pod"`
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Labels map[string]string `json:"labels,omitempty"`

	Times []int64 `json:"times"`
	Ends  []int64 `json:"ends,omitempty"`

	// A null is a gap: a series the dictionary knows that the scrape did not
	// report. It is not a zero.
	Values  readings `json:"values"`
	Min     readings `json:"min,omitempty"`
	Max     readings `json:"max,omitempty"`
	Last    readings `json:"last,omitempty"`
	Samples []int    `json:"samples,omitempty"`
}

// readings is a column of points, with a gap as null.
//
// The type exists rather than a plain []*float64 because of how this service
// writes a response. httpx.WriteJSON sets the status before encoding, so a
// value encoding/json refuses does not produce a 500 — it produces a 200 with
// a truncated body and a line in the log, which is the least actionable
// failure available. NaN is exactly such a value, and it is the writer's own
// encoding for a gap.
//
// podstats.decodeRows already converts every gap to nil, so nothing should
// reach here. This makes that structural rather than a convention: any column
// declared with this type cannot break a response, whatever produced it.
type readings []*float64

func (r readings) MarshalJSON() ([]byte, error) {
	out := make([]*float64, len(r))
	for i, f := range r {
		if f == nil || math.IsNaN(*f) || math.IsInf(*f, 0) {
			continue
		}
		out[i] = f
	}
	return json.Marshal(out)
}

type statsSeriesResponse struct {
	DeploymentID string    `json:"deploymentId"`
	Tier         string    `json:"tier"`
	Step         string    `json:"step"`
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`

	Series    []statsSeries  `json:"series"`
	Warnings  []statsWarning `json:"warnings"`
	Truncated bool           `json:"truncated"`
}

// series serves the data.
//
//	@Summary		Read stats series for a deployment
//	@Description	Points for the named metrics, per pod, decoded against each pod's dictionary.
//	@Description
//	@Description	metric is required, and repeatable. It is what bounds the response: the rows
//	@Description	are stored positionally, so a query with no name filter would read every
//	@Description	series of every pod. Use the metrics route to find the names.
//	@Description
//	@Description	A counter's value is its growth over the interval ending at that point, on
//	@Description	both tiers, so a chart does not have to know which tier answered. Its closing
//	@Description	cumulative reading is available as the last stat on the rollup tier, and
//	@Description	counters=absolute returns the raw readings instead. A counter that went
//	@Description	backwards is treated as a process restart rather than a negative delta.
//	@Description
//	@Description	tier=auto picks live when the window fits inside the live tier's reach and
//	@Description	rollup otherwise, because a window older than that cannot be answered from
//	@Description	live rows however many are read. The resolved tier and its step are echoed
//	@Description	back.
//	@Description
//	@Description	times are unix milliseconds, oldest first, and the columns are parallel. A
//	@Description	null value is a gap rather than a zero. On the rollup tier ends is carried
//	@Description	too, because rows are not contiguous: when a bucket's end does not meet the
//	@Description	next one's start, scraping had stopped in between.
//	@Tags			stats
//	@Produce		json
//	@Param			deploymentId	path		string		true	"The deployment to read"
//	@Param			metric			query		[]string	true	"Metric names, matched exactly; repeat the parameter for more than one"	collectionFormat(multi)
//	@Param			label			query		[]string	false	"Narrow within those metrics, as key=value; repeated labels are ANDed"	collectionFormat(multi)
//	@Param			pod				query		[]string	false	"Only these pods; repeat the parameter for more than one"				collectionFormat(multi)
//	@Param			tier			query		string		false	"auto, live or rollup"													default(auto)	Enums(auto, live, rollup)
//	@Param			from			query		string		false	"Start of the window, RFC3339. Defaults to 15 minutes before to"
//	@Param			to				query		string		false	"End of the window, RFC3339. Defaults to now"
//	@Param			stats			query		string		false	"Which numbers to return, comma separated: value, min, max, last, samples. Rollup tier only"	default(value)
//	@Param			counters		query		string		false	"delta for growth, absolute for the raw cumulative reading"										default(delta)	Enums(delta, absolute)
//	@Param			limit			query		int			false	"Points per series, clamped to 1..5000"															default(1000)
//	@Success		200				{object}	statsSeriesResponse
//	@Failure		400				{object}	httpx.ErrorResponse	"no metric was named, a time is not RFC3339, or a parameter is not one of its values"
//	@Failure		500				{object}	httpx.ErrorResponse
//	@Router			/stats/{deploymentId}/series [get]
func (h *StatsHandler) series(w http.ResponseWriter, r *http.Request) {
	deploymentID := r.PathValue("deploymentId")

	q, err := h.parseStatsQuery(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	q.DeploymentID = deploymentID

	result, err := h.r.Series(r.Context(), q)
	if err != nil {
		slog.Error("api: read stats series", "deployment", deploymentID, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to read pod stats")
		return
	}

	out := make([]statsSeries, 0, len(result.Series))
	for _, s := range result.Series {
		out = append(out, statsSeries{
			Pod: s.Pod, Name: s.Name, Kind: s.Kind.String(), Labels: s.Labels,
			Times: s.TimesMS, Ends: s.EndsMS,
			Values: s.Values, Min: s.Min, Max: s.Max, Last: s.Last, Samples: s.Samples,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, statsSeriesResponse{
		DeploymentID: deploymentID,
		Tier:         string(result.Tier),
		Step:         result.Step.String(),
		From:         q.From.UTC(),
		To:           q.To.UTC(),
		Series:       out,
		Warnings:     encodeWarnings(result.Warnings),
		Truncated:    result.Truncated,
	})
}

// parseStatsQuery builds a query from the request, validating everything typed
// and rejecting a request with no metric filter.
func (h *StatsHandler) parseStatsQuery(r *http.Request) (podstats.Query, error) {
	query := r.URL.Query()

	names := nonEmpty(query["metric"])
	if len(names) == 0 {
		return podstats.Query{}, errInvalid("metric is required: name at least one " +
			"metric to read. GET /stats/{deploymentId}/metrics lists them")
	}
	if len(names) > statsMaxMetrics {
		return podstats.Query{}, errInvalid("at most " + strconv.Itoa(statsMaxMetrics) +
			" metric parameters")
	}

	labels, err := parseLabels(query["label"])
	if err != nil {
		return podstats.Query{}, err
	}

	tier, err := parseTier(query.Get("tier"))
	if err != nil {
		return podstats.Query{}, err
	}

	from, to, err := h.statsWindow(query)
	if err != nil {
		return podstats.Query{}, err
	}

	stats, err := parseStats(query.Get("stats"))
	if err != nil {
		return podstats.Query{}, err
	}

	counters, err := parseCounters(query.Get("counters"))
	if err != nil {
		return podstats.Query{}, err
	}

	limit := statsDefaultPoints
	if raw := query.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return podstats.Query{}, errInvalid("limit must be an integer")
		}
		limit = n
	}
	switch {
	case limit < 1:
		limit = 1
	case limit > statsMaxPoints:
		limit = statsMaxPoints
	}

	return podstats.Query{
		Pods:     nonEmpty(query["pod"]),
		Selector: podstats.Selector{Names: names, Labels: labels},
		Tier:     tier,
		From:     from,
		To:       to,
		Projection: podstats.Projection{
			Stats: stats, Counters: counters, Limit: limit,
		},
	}, nil
}

// statsWindow defaults the window the way the traces list does: either bound
// may be given alone.
func (h *StatsHandler) statsWindow(query map[string][]string) (time.Time, time.Time, error) {
	get := func(key string) string {
		if v := query[key]; len(v) > 0 {
			return v[0]
		}
		return ""
	}

	from, err := parseTime(get("from"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := parseTime(get("to"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	end := h.now()
	if to != nil {
		end = *to
	}
	start := end.Add(-statsDefaultWindow)
	if from != nil {
		start = *from
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, errInvalid("to must not be before from")
	}
	return start, end, nil
}

// parseLabels reads repeated key=value parameters into a filter.
func parseLabels(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	out := make(map[string]string, len(raw))
	for _, pair := range raw {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			return nil, errInvalid("label must be key=value: " + pair)
		}
		out[key] = value
	}
	return out, nil
}

func parseTier(raw string) (podstats.Tier, error) {
	switch podstats.Tier(raw) {
	case "":
		return podstats.TierAuto, nil
	case podstats.TierAuto, podstats.TierLive, podstats.TierRollup:
		return podstats.Tier(raw), nil
	default:
		return "", errInvalid("tier must be auto, live or rollup: " + raw)
	}
}

func parseCounters(raw string) (podstats.Counters, error) {
	switch podstats.Counters(raw) {
	case "":
		return podstats.CountersDelta, nil
	case podstats.CountersDelta, podstats.CountersAbsolute:
		return podstats.Counters(raw), nil
	default:
		return "", errInvalid("counters must be delta or absolute: " + raw)
	}
}

// parseStats reads the projection, which is the "not a whole projection" knob:
// a caller charting a line should not be sent four columns it will discard.
func parseStats(raw string) ([]podstats.Stat, error) {
	if strings.TrimSpace(raw) == "" {
		return []podstats.Stat{podstats.StatValue}, nil
	}

	var out []podstats.Stat
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		stat := podstats.Stat(name)
		if !knownStat(stat) {
			return nil, errInvalid("unknown stat " + name + ": expected one of " +
				statNames())
		}
		out = append(out, stat)
	}
	if len(out) == 0 {
		return []podstats.Stat{podstats.StatValue}, nil
	}
	return out, nil
}

func knownStat(s podstats.Stat) bool {
	for _, known := range podstats.KnownStats {
		if known == s {
			return true
		}
	}
	return false
}

func statNames() string {
	names := make([]string, 0, len(podstats.KnownStats))
	for _, s := range podstats.KnownStats {
		names = append(names, string(s))
	}
	return strings.Join(names, ", ")
}

// nonEmpty drops blank repeats, so ?metric=&metric=x reads as one name rather
// than as one name and one that matches nothing.
func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func encodeWarnings(warnings []podstats.Warning) []statsWarning {
	out := make([]statsWarning, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, statsWarning{Pod: w.Pod, Reason: w.Reason})
	}
	return out
}
