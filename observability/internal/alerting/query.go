package alerting

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Scope narrows the rows a query reads. A zero field is no constraint on that
// axis, which is the same contract repo.LogFilter already has — the log fields
// here are named after it deliberately, so the fetcher is a translation rather
// than a mapping.
type Scope struct {
	DeploymentID  string `json:"deploymentId,omitempty"`
	IntegrationID string `json:"integrationId,omitempty"`
	AppName       string `json:"appName,omitempty"`
	AppVersion    string `json:"appVersion,omitempty"`

	// Logs only.
	Levels []string `json:"levels,omitempty"`
	Search string   `json:"search,omitempty"`

	// Pod stats only. Across says how the per-pod series of one deployment
	// collapse to one number, because a watch is about the deployment and the
	// pods underneath it come and go.
	Pods   []string          `json:"pods,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	Across Aggregate         `json:"across,omitempty"`
}

// key renders the scope into something comparable, for coalescing. Sorted,
// because two scopes that differ only in the order somebody typed their log
// levels are the same scope and must share one fetch.
func (s Scope) key() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%s|%s|", s.DeploymentID, s.IntegrationID, s.AppName, s.AppVersion)
	b.WriteString(strings.Join(sorted(s.Levels), ",") + "|" + s.Search + "|")
	b.WriteString(strings.Join(sorted(s.Pods), ",") + "|")
	for _, k := range sortedKeys(s.Labels) {
		b.WriteString(k + "=" + s.Labels[k] + ";")
	}
	b.WriteString("|" + string(s.Across))
	return b.String()
}

// Query is one fetch: what number, over which rows, at what resolution, across
// which span.
//
// From and To are bucket boundaries already. The caller truncates before building
// one, so a fetcher never has to know about the evaluation lag or about which end
// of a bucket a store stamps its rows with.
//
// A condition asks for its whole span in one Query — window, guard band and
// baseline together — rather than one query per region. Two fetches of adjacent
// ranges cost two index scans to answer a question one scan answers, and they can
// disagree at the seam if a row lands between them.
type Query struct {
	Source    Source
	Metric    string
	Aggregate Aggregate
	Scope     Scope
	Step      time.Duration
	From, To  time.Time
}

// Key is what makes two queries the same fetch.
//
// Everything that changes which rows are read is in it. The time range is not:
// two conditions over the same rows at the same resolution differing only in how
// far back they look are coalesced into one fetch of the union, which is why
// Series is located by time rather than by offset from its end.
func (q Query) Key() string {
	return strings.Join([]string{
		string(q.Source), q.Metric, string(q.Aggregate), q.Step.String(), q.Scope.key(),
	}, "\x00")
}

// Widen returns the query covering both spans. Used when coalescing: the union
// is always contiguous because both queries share a step and both bounds are
// aligned to it.
func (q Query) Widen(other Query) Query {
	if other.From.Before(q.From) {
		q.From = other.From
	}
	if other.To.After(q.To) {
		q.To = other.To
	}
	return q
}

// Buckets is how many buckets the query spans.
func (q Query) Buckets() int { return BucketCount(q.From, q.To, q.Step) }

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func sortedKeys(in map[string]string) []string {
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
