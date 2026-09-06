package podstats

import (
	"context"
	"sort"
	"strings"
	"time"
)

const (
	// readTimeout bounds a whole query, every round trip included: the caller
	// cares about the request rather than the stages. It is the only bound that
	// exists, since logs/main.go sets ReadHeaderTimeout and nothing else.
	readTimeout = 5 * time.Second

	// podLookback is how far before a window's start a pod may have last
	// written and still be worth reading. One rollup interval, because a bucket
	// is stamped with its start and written at its end.
	podLookback = time.Hour

	// reportingWindow is how stale a pod's last write may be and still count as
	// reporting.
	reportingFactor = 3
)

// Service answers deployment-scoped questions about pod stats.
type Service struct {
	reader *Reader
}

// NewService returns a Service reading through r.
func NewService(r *Reader) *Service {
	return &Service{reader: r}
}

// PodStatus is one pod as the API describes it.
type PodStatus struct {
	Pod       string
	LastSeen  time.Time
	Reporting bool
	Meta      Meta

	// LiveRows and RollupRows are reported separately because zero live rows
	// beside a full history is the normal state of a pod that stopped a few
	// hours ago — the live tier's TTL is only twice the rollup interval. Shown
	// together, that reads as expected rather than as a fault.
	LiveRows   int64
	RollupRows int64

	Series int
}

// Warning is one pod that could not be answered for, and why. A pod is skipped
// rather than failing the request: a deployment's other pods are still worth
// returning, and the reason is worth saying out loud.
type Warning struct {
	Pod    string
	Reason string
}

// Pods describes every pod of a deployment that the index still holds.
//
// An unknown deployment is an empty list, never a 404. This service has no
// deployment registry, so it cannot tell a deployment that never existed from
// one whose sidecar is switched off or whose stats have expired.
func (s *Service) Pods(ctx context.Context, deploymentID string) ([]PodStatus, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	refs, truncated, err := s.reader.Pods(ctx, deploymentID, time.Time{})
	if err != nil {
		return nil, false, err
	}
	if len(refs) == 0 {
		return nil, false, nil
	}

	states, err := s.reader.States(ctx, deploymentID, refs, TierLive)
	if err != nil {
		return nil, false, err
	}

	now := time.Now()
	out := make([]PodStatus, 0, len(refs))
	for _, ref := range refs {
		state := states[ref.Name]
		status := PodStatus{
			Pod:        ref.Name,
			LastSeen:   ref.LastSeen,
			Meta:       state.Meta,
			LiveRows:   state.LiveRows,
			RollupRows: state.RollupRows,
		}
		status.Meta.Gen = state.Gen

		if step := state.Meta.Step(TierLive); step > 0 {
			status.Reporting = now.Sub(ref.LastSeen) <= reportingFactor*step
		}

		// The dictionary is what says how many series a pod exposes, and it is
		// one hash read per pod. Worth it: without it a caller cannot tell a
		// pod that is reporting nothing from one that is not reporting.
		if dict, err := s.reader.Dictionary(ctx, deploymentID, ref.Name, state); err == nil {
			status.Series = len(dict)
		}
		out = append(out, status)
	}
	return out, truncated, nil
}

// Metric is one metric name and the label sets it appears under.
type Metric struct {
	Name   string
	Kind   Kind
	Series []MetricSeries
}

// MetricSeries is one label set of a metric, and the pods exposing it.
type MetricSeries struct {
	Labels map[string]string
	Pods   []string
}

// Metrics is the catalogue: every series a deployment's pods describe, with no
// rows read.
//
// It exists so a caller can find exact metric names before asking for data,
// which is what makes the required name filter on Series usable rather than a
// guessing game. Bounded by construction — a dictionary is about a hundred
// entries and there are at most a few dozen pods.
//
// The truncation flag is reported rather than swallowed. A catalogue built
// from a capped pod list is partial, and a partial catalogue reads exactly
// like a complete one: a caller cannot otherwise tell a metric no pod exposes
// from one exposed only by a pod the cap dropped.
func (s *Service) Metrics(ctx context.Context, deploymentID string, pods []string, prefix string) ([]Metric, []Warning, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	refs, truncated, err := s.reader.Pods(ctx, deploymentID, time.Time{})
	if err != nil {
		return nil, nil, false, err
	}
	refs = keepPods(refs, pods)
	if len(refs) == 0 {
		return nil, nil, truncated, nil
	}

	states, err := s.reader.States(ctx, deploymentID, refs, TierLive)
	if err != nil {
		return nil, nil, false, err
	}

	// Keyed by name then by label set, so a histogram reads as one metric with
	// a series per boundary rather than as a hundred unrelated names.
	type group struct {
		kind   Kind
		byKey  map[string]*MetricSeries
		labels []string
	}
	groups := map[string]*group{}
	var warnings []Warning

	for _, ref := range refs {
		dict, err := s.reader.Dictionary(ctx, deploymentID, ref.Name, states[ref.Name])
		if err != nil {
			return nil, nil, false, err
		}
		if len(dict) == 0 {
			warnings = append(warnings, Warning{Pod: ref.Name, Reason: "no dictionary"})
			continue
		}

		for _, entry := range dict {
			if prefix != "" && !strings.HasPrefix(entry.Name, prefix) {
				continue
			}
			g, ok := groups[entry.Name]
			if !ok {
				g = &group{kind: entry.Kind, byKey: map[string]*MetricSeries{}}
				groups[entry.Name] = g
			}
			key := labelKey(entry.Labels)
			ms, ok := g.byKey[key]
			if !ok {
				ms = &MetricSeries{Labels: entry.Labels}
				g.byKey[key] = ms
				g.labels = append(g.labels, key)
			}
			ms.Pods = append(ms.Pods, ref.Name)
		}
	}

	out := make([]Metric, 0, len(groups))
	for name, g := range groups {
		sort.Strings(g.labels)
		metric := Metric{Name: name, Kind: g.kind}
		for _, key := range g.labels {
			ms := g.byKey[key]
			sort.Strings(ms.Pods)
			metric.Series = append(metric.Series, *ms)
		}
		out = append(out, metric)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, warnings, truncated, nil
}

// Query is one request for series data.
type Query struct {
	DeploymentID string
	Pods         []string
	Selector     Selector
	Tier         Tier
	From         time.Time
	To           time.Time
	Projection   Projection
}

// Result is a query's answer.
type Result struct {
	// Tier is the resolved tier, never auto: a caller charting the answer needs
	// to know which one it got, because the step differs.
	Tier      Tier
	Step      time.Duration
	Series    []Series
	Warnings  []Warning
	Truncated bool
}

// Series answers a query.
//
// A pod that cannot be read is warned about and skipped, never fatal. The
// states this covers are ordinary rather than exceptional: a pod whose live
// tier has expired while it stays in the index for eight days, or one whose
// dictionary went with it.
func (s *Service) Series(ctx context.Context, q Query) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	refs, truncated, err := s.reader.Pods(ctx, q.DeploymentID, q.From.Add(-podLookback))
	if err != nil {
		return Result{}, err
	}
	refs = keepPods(refs, q.Pods)
	if len(refs) == 0 {
		return Result{Tier: resolved(q.Tier), Series: nil}, nil
	}

	// Resolving the tier needs a pod's configuration, so a first pass reads the
	// states against the tier the caller asked for and a second only happens if
	// auto picks the other one.
	states, err := s.reader.States(ctx, q.DeploymentID, refs, resolved(q.Tier))
	if err != nil {
		return Result{}, err
	}

	tier := q.Tier
	if tier == TierAuto {
		tier = pickTier(states, q.From)
		if tier != TierLive {
			if states, err = s.reader.States(ctx, q.DeploymentID, refs, tier); err != nil {
				return Result{}, err
			}
		}
	}

	result := Result{Tier: tier, Truncated: truncated}
	for _, ref := range refs {
		state := states[ref.Name]
		if state.Meta.Step(tier) > result.Step {
			result.Step = state.Meta.Step(tier)
		}

		dict, err := s.reader.Dictionary(ctx, q.DeploymentID, ref.Name, state)
		if err != nil {
			return Result{}, err
		}
		if len(dict) == 0 {
			result.Warnings = append(result.Warnings,
				Warning{Pod: ref.Name, Reason: "dictionary unavailable"})
			continue
		}

		// Resolved before any row is read, so a pod exposing none of the named
		// metrics costs nothing beyond the dictionary already fetched.
		indices := q.Selector.Resolve(dict)
		if len(indices) == 0 {
			result.Warnings = append(result.Warnings,
				Warning{Pod: ref.Name, Reason: "no matching series"})
			continue
		}

		rows, err := s.reader.Rows(ctx, q.DeploymentID, ref.Name, state, Window{
			Tier: tier, From: q.From, To: q.To,
		})
		if err != nil {
			return Result{}, err
		}
		if len(rows) == 0 {
			result.Warnings = append(result.Warnings,
				Warning{Pod: ref.Name, Reason: "no rows in window"})
			continue
		}

		result.Series = append(result.Series, decodeRows(ref.Name, tier, rows, dict,
			indices, q.From.UnixMilli(), q.To.UnixMilli(), q.Projection)...)
	}

	sort.SliceStable(result.Series, func(i, j int) bool {
		if result.Series[i].Name != result.Series[j].Name {
			return result.Series[i].Name < result.Series[j].Name
		}
		return result.Series[i].Pod < result.Series[j].Pod
	})
	return result, nil
}

// pickTier chooses between the tiers for a window.
//
// Live only when every pod can actually reach back that far. A window older
// than liveDepth × sampleInterval cannot be answered from live rows however
// many are read, so answering it from live would silently return a shorter
// series than was asked for — which looks like data ending rather than like a
// tier that does not go back that far.
func pickTier(states map[string]PodState, from time.Time) Tier {
	if len(states) == 0 {
		return TierLive
	}
	for _, state := range states {
		reach := state.Meta.LiveReach()
		if reach == 0 || time.Since(from) > reach {
			return TierRollup
		}
	}
	return TierLive
}

// resolved maps auto onto the tier to probe with first.
func resolved(t Tier) Tier {
	if t == TierRollup {
		return TierRollup
	}
	return TierLive
}

// keepPods narrows a pod list to the names asked for, keeping index order. An
// empty filter keeps everything.
func keepPods(refs []PodRef, names []string) []PodRef {
	if len(names) == 0 {
		return refs
	}
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[name] = struct{}{}
	}

	// A fresh slice rather than refs[:0]: reusing the backing array would
	// leave the caller's slice holding a rearranged prefix of itself.
	out := make([]PodRef, 0, len(refs))
	for _, ref := range refs {
		if _, ok := wanted[ref.Name]; ok {
			out = append(out, ref)
		}
	}
	return out
}
