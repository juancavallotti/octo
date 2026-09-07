package source

import (
	"fmt"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

// The projections. Each returns the SELECT list for one metric and aggregate,
// and whether the result carries a denominator alongside the value.
//
// They are switches over closed catalogues rather than templates over a column
// name, and that is deliberate on two counts. It keeps a stored watch definition
// from ever reaching the planner as text; and it puts the decision about what
// "error rate" means — which rows count as failures — in one readable place per
// source, rather than distributed across a format string and a caller.

// failedPredicate is what counts as a failed trace.
//
// `status <> 'ok'` rather than `status = 'failed'`, matching the partial index
// idx_trace_summaries_failed that serves the same question for the UI. A status
// this service has not seen before is a trace that did not succeed, and counting
// it as a success is the direction that hides an incident.
const failedPredicate = `status <> 'ok'`

func traceProjection(metric string, agg alerting.Aggregate) (string, bool, error) {
	switch metric {
	case "traces":
		return `count(*)::double precision`, false, nil
	case "failed_traces":
		return `count(*) FILTER (WHERE ` + failedPredicate + `)::double precision`, false, nil
	case "error_rate":
		// One scan, two counts. Fetching the numerator and denominator
		// separately would cost two index range scans to answer what one
		// answers, and they could disagree at the seam if a row landed between
		// them.
		return `count(*) FILTER (WHERE ` + failedPredicate + `)::double precision,
       count(*)::double precision`, true, nil
	case "duration_ns":
		return durationProjection(agg)
	case "cost_usd":
		return `sum(cost_usd)::double precision`, false, nil
	case "tokens":
		return `sum(input_tokens + output_tokens)::double precision`, false, nil
	case "llm_calls":
		return `sum(llm_calls)::double precision`, false, nil
	case "unpriced_calls":
		return `sum(unpriced_calls)::double precision`, false, nil
	default:
		return "", false, fmt.Errorf("source: unknown trace metric %q", metric)
	}
}

func durationProjection(agg alerting.Aggregate) (string, bool, error) {
	switch agg {
	case alerting.AggP95:
		return `percentile_cont(0.95) WITHIN GROUP (ORDER BY root_duration_ns)::double precision`, false, nil
	case alerting.AggAvg:
		return `avg(root_duration_ns)::double precision`, false, nil
	case alerting.AggMax:
		return `max(root_duration_ns)::double precision`, false, nil
	default:
		return "", false, fmt.Errorf("source: %w: duration_ns does not take %s", alerting.ErrInvalidParams, agg)
	}
}

// errorLevels is what counts as an error in the log stream. Lowercased on both
// sides, because the level a runtime ships is whatever its logger was configured
// to emit and casing is not a distinction anybody meant to make.
const errorLevels = `('error', 'fatal')`

func logProjection(metric string, _ alerting.Aggregate) (string, bool, error) {
	switch metric {
	case "events":
		return `count(*)::double precision`, false, nil
	case "error_rate":
		return `count(*) FILTER (WHERE lower(level) IN ` + errorLevels + `)::double precision,
       count(*)::double precision`, true, nil
	default:
		return "", false, fmt.Errorf("source: unknown log metric %q", metric)
	}
}
