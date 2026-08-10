package ingest

import (
	"encoding/json"
	"testing"
	"time"
)

// The payloads below are verbatim output from the runtime's own trace encoder
// (runtime/services/k8s/tracesink.go), not JSON written to suit these tests. The
// two modules do not share a go.mod, so this decoder is a hand-kept copy of a
// wire format that lives elsewhere; pinning it against what the producer actually
// emits is the only thing that catches the two drifting apart.
const (
	wireBlockPostInvoke = `{"seq":12,"kind":"block.post-invoke","traceId":"t1","eventId":"e1",` +
		`"flow":"orders","path":"orders.charge","blockType":"rest","at":"2026-08-09T12:00:00.5Z",` +
		`"durationNs":1500000,"body":{"id":7},"vars":{"tenant":"acme"},` +
		`"deploymentId":"11111111-1111-1111-1111-111111111111","appName":"orders","appVersion":"v3"}`

	wireLLMTurn = `{"seq":13,"kind":"llm.turn","traceId":"t1","eventId":"e1","flow":"orders",` +
		`"path":"orders.agent","blockType":"ai-agent","at":"2026-08-09T12:00:00.5Z","durationNs":900000000,` +
		`"attrs":{"block":"agent","connector":"myClaude","provider":"ANTHROPIC","iteration":2,` +
		`"model":"claude-3-5-sonnet-20241022",` +
		`"stopReason":"end_turn","toolCalls":1,` +
		`"usage":{"cachedTokens":800,"inputTokens":1200,"outputTokens":340,"thinkingTokens":120}},` +
		`"deploymentId":"11111111-1111-1111-1111-111111111111","appName":"orders","appVersion":"v3"}`

	// A turn whose provider reported nothing: the runtime omits the usage object
	// entirely rather than sending zeros.
	wireLLMTurnNoUsage = `{"seq":14,"kind":"llm.turn","traceId":"t1","at":"2026-08-09T12:00:00.5Z",` +
		`"attrs":{"connector":"myClaude","model":"claude-3-5-sonnet-20241022"},` +
		`"deploymentId":"11111111-1111-1111-1111-111111111111","appName":"orders","appVersion":"v3"}`

	wireLLMEmbed = `{"seq":15,"kind":"llm.embed","traceId":"t1","at":"2026-08-09T12:00:00.5Z",` +
		`"attrs":{"batch":4,"connector":"oa","model":"text-embedding-3-small","usage":{"inputTokens":900}},` +
		`"deploymentId":"11111111-1111-1111-1111-111111111111","appName":"orders","appVersion":"v3"}`

	// The dropped marker carries neither a trace id nor a real timestamp: the
	// runtime builds it with a sequence, a kind and a count and nothing else.
	wireTraceDropped = `{"seq":99,"kind":"trace.dropped","at":"0001-01-01T00:00:00Z",` +
		`"attrs":{"dropped":5,"total":5},` +
		`"deploymentId":"11111111-1111-1111-1111-111111111111","appName":"orders","appVersion":"v3"}`

	wireSourceReceive = `{"seq":1,"kind":"source.receive","traceId":"t1","eventId":"e1",` +
		`"at":"2026-08-09T12:00:00.5Z","attrs":{"method":"POST","route":"/orders/{id}","status":0},` +
		`"deploymentId":"11111111-1111-1111-1111-111111111111","appName":"orders","appVersion":"v3"}`

	wireFlowFailed = `{"seq":20,"kind":"flow.failed","traceId":"t1","eventId":"e1","flow":"orders",` +
		`"at":"2026-08-09T12:00:00.5Z","durationNs":2000000,"error":"boom","attrs":{"block":"rest charge"},` +
		`"deploymentId":"11111111-1111-1111-1111-111111111111","appName":"orders","appVersion":"v3"}`
)

func mustParse(t *testing.T, wire string) TraceRecord {
	t.Helper()
	got, err := parseTraceRecord([]byte(wire))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return got
}

func TestParseTraceRecordFlatEnvelope(t *testing.T) {
	got := mustParse(t, wireBlockPostInvoke)

	want := time.Date(2026, 8, 9, 12, 0, 0, 500000000, time.UTC)
	switch {
	case got.DeploymentID != "11111111-1111-1111-1111-111111111111":
		t.Errorf("deployment = %q", got.DeploymentID)
	case got.AppName != "orders" || got.AppVersion != "v3":
		t.Errorf("app = %s/%s, want orders/v3", got.AppName, got.AppVersion)
	case got.Kind != KindBlockPostInvoke:
		t.Errorf("kind = %q, want %q", got.Kind, KindBlockPostInvoke)
	case got.Seq != 12:
		t.Errorf("seq = %d, want 12", got.Seq)
	case got.TraceID != "t1" || got.EventID != "e1":
		t.Errorf("ids = %s/%s, want t1/e1", got.TraceID, got.EventID)
	case got.Flow != "orders" || got.Path != "orders.charge" || got.BlockType != "rest":
		t.Errorf("address = %s/%s/%s", got.Flow, got.Path, got.BlockType)
	case !got.Time.Equal(want):
		t.Errorf("time = %s, want %s", got.Time, want)
	case got.DurationNs != 1500000:
		t.Errorf("duration = %d, want 1500000", got.DurationNs)
	case string(got.Body) != `{"id":7}`:
		t.Errorf("body = %s", got.Body)
	case string(got.Vars) != `{"tenant":"acme"}`:
		t.Errorf("vars = %s", got.Vars)
	}
}

// The interval a record occupies is [Time - DurationNs, Time], because the
// runtime stamps a post-invoke at completion. Everything reading a trace as a
// timeline depends on that, so it is asserted against a real payload.
func TestParseTraceRecordCarriesACompletedInterval(t *testing.T) {
	got := mustParse(t, wireBlockPostInvoke)

	// The record is stamped at 12:00:00.5 and took 1.5ms, so it began at
	// 12:00:00.4985.
	start := got.Time.Add(-time.Duration(got.DurationNs))
	want := time.Date(2026, 8, 9, 12, 0, 0, 498500000, time.UTC)
	if !start.Equal(want) {
		t.Fatalf("interval starts at %s, want %s", start, want)
	}
}

func TestParseTraceRecordModelCall(t *testing.T) {
	got := mustParse(t, wireLLMTurn)

	if got.Model != "claude-3-5-sonnet-20241022" {
		t.Fatalf("model = %q", got.Model)
	}
	if got.Usage == nil {
		t.Fatal("usage did not survive the decode")
	}
	if got.Usage.InputTokens != 1200 || got.Usage.OutputTokens != 340 ||
		got.Usage.ThinkingTokens != 120 || got.Usage.CachedTokens != 800 {
		t.Fatalf("usage = %+v", *got.Usage)
	}

	call, isCall := got.ModelCall()
	if !isCall {
		t.Fatal("an llm.turn is not reported as a model call")
	}
	if call.Embedding {
		t.Error("a completion was reported as an embedding")
	}
	if call.Model != got.Model || call.Usage != got.Usage {
		t.Errorf("call = %+v, want it to carry the record's model and usage", call)
	}
	// The vendor family, distinct from the authored connector name beside it. The
	// consumer keys its cached-token arithmetic on this rather than inferring the
	// vendor from the model id.
	if got.Provider != "ANTHROPIC" {
		t.Errorf("provider = %q, want ANTHROPIC", got.Provider)
	}
	if call.Provider != got.Provider {
		t.Errorf("call provider = %q, want it to carry the record's %q", call.Provider, got.Provider)
	}
}

// A record from a runtime that stamped no provider — everything stored before
// the attribute existed — parses to an empty one rather than failing, and
// pricing falls back to inferring from the model id as it always did.
func TestParseTraceRecordWithoutAProvider(t *testing.T) {
	got := mustParse(t, wireLLMTurnNoUsage)
	if got.Provider != "" {
		t.Errorf("provider = %q, want empty for a record that carried none", got.Provider)
	}
	call, isCall := got.ModelCall()
	if !isCall || call.Provider != "" {
		t.Errorf("call = %+v, want a model call with no provider", call)
	}
}

// Embeddings are billed from a different rate card and carry no output or cache
// accounting, so pricing one as a completion would be wrong in a way the
// arithmetic never reveals.
func TestParseTraceRecordEmbeddingIsFlagged(t *testing.T) {
	got := mustParse(t, wireLLMEmbed)

	call, isCall := got.ModelCall()
	if !isCall {
		t.Fatal("an llm.embed is not reported as a model call")
	}
	if !call.Embedding {
		t.Fatal("an embedding was not flagged as one")
	}
	if got.Model != "text-embedding-3-small" {
		t.Errorf("model = %q", got.Model)
	}
	if got.Usage == nil || got.Usage.InputTokens != 900 {
		t.Fatalf("usage = %+v, want 900 input tokens", got.Usage)
	}
	if got.Usage.OutputTokens != 0 || got.Usage.CachedTokens != 0 {
		t.Errorf("an embedding decoded output or cache tokens it never carried: %+v", *got.Usage)
	}
}

// The runtime omits attrs.usage entirely when the provider reported nothing. That
// has to survive as nil, because nil prices as "no usage" while a zeroed Usage
// would price as a call that genuinely cost nothing.
func TestParseTraceRecordAbsentUsageStaysAbsent(t *testing.T) {
	got := mustParse(t, wireLLMTurnNoUsage)

	if got.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("model = %q", got.Model)
	}
	if got.Usage != nil {
		t.Fatalf("absent usage decoded as %+v, want nil", *got.Usage)
	}
	call, _ := got.ModelCall()
	if call.Usage != nil {
		t.Fatal("the priced call claims usage the provider never reported")
	}
}

// A record that is not a model call must not be offered for pricing, whatever it
// happens to carry in attrs.
func TestParseTraceRecordNonModelKindsAreNotPriced(t *testing.T) {
	for _, wire := range []string{wireBlockPostInvoke, wireSourceReceive, wireFlowFailed, wireTraceDropped} {
		got := mustParse(t, wire)
		if _, isCall := got.ModelCall(); isCall {
			t.Errorf("%s was offered for pricing", got.Kind)
		}
		if got.Model != "" || got.Usage != nil {
			t.Errorf("%s decoded model accounting: %q %+v", got.Kind, got.Model, got.Usage)
		}
	}
}

// The dropped marker is about the stream, not about a trace: it carries no trace
// id, and no timestamp either. It is stamped on arrival so the column is never
// null and the marker sits where it was actually observed.
func TestParseTraceRecordStampsTheDroppedMarker(t *testing.T) {
	before := time.Now()
	got := mustParse(t, wireTraceDropped)

	if got.Kind != KindTraceDropped {
		t.Fatalf("kind = %q", got.Kind)
	}
	if got.TraceID != "" {
		t.Errorf("trace id = %q, want empty — the marker belongs to no trace", got.TraceID)
	}
	if got.Time.IsZero() {
		t.Fatal("the marker was stored with a zero timestamp")
	}
	if got.Time.Before(before) {
		t.Fatalf("stamped %s, want a time at or after arrival (%s)", got.Time, before)
	}

	var attrs map[string]any
	if err := json.Unmarshal(got.Attrs, &attrs); err != nil {
		t.Fatalf("attrs: %v", err)
	}
	if attrs["dropped"] != float64(5) {
		t.Errorf("dropped count = %v, want 5", attrs["dropped"])
	}
}

// A real timestamp must never be replaced by arrival time, or every interval in
// the trace shifts to whenever the aggregator happened to be reading.
func TestParseTraceRecordKeepsARealTimestamp(t *testing.T) {
	got := mustParse(t, wireSourceReceive)

	want := time.Date(2026, 8, 9, 12, 0, 0, 500000000, time.UTC)
	if !got.Time.Equal(want) {
		t.Fatalf("time = %s, want the record's own %s", got.Time, want)
	}
}

// The store keeps what a runtime said. A kind this build has never heard of is a
// newer runtime talking, not a corrupt record.
func TestParseTraceRecordKeepsUnknownKinds(t *testing.T) {
	wire := `{"seq":1,"kind":"block.something-new","traceId":"t1","at":"2026-08-09T12:00:00.5Z",` +
		`"deploymentId":"11111111-1111-1111-1111-111111111111"}`

	got := mustParse(t, wire)
	if got.Kind != "block.something-new" {
		t.Fatalf("kind = %q, want it kept verbatim", got.Kind)
	}
}

func TestParseTraceRecordRejects(t *testing.T) {
	cases := []struct {
		name string
		wire string
	}{
		{
			name: "a record with no deployment id could not be attributed",
			wire: `{"seq":1,"kind":"flow.started","at":"2026-08-09T12:00:00.5Z"}`,
		},
		{
			name: "a record with no kind could not be interpreted",
			wire: `{"seq":1,"at":"2026-08-09T12:00:00.5Z","deploymentId":"11111111-1111-1111-1111-111111111111"}`,
		},
		{
			name: "a body that is not JSON is not a record",
			wire: `{"seq":1,"kind":"flow.started"`,
		},
		{
			name: "a record whose timestamp is not a timestamp",
			wire: `{"seq":1,"kind":"flow.started","at":"whenever","deploymentId":"11111111-1111-1111-1111-111111111111"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseTraceRecord([]byte(tc.wire)); err == nil {
				t.Fatal("parse succeeded, want an error")
			}
		})
	}
}

// Usage that cannot be read must not cost a record its whole row. Pricing it as
// "the provider reported nothing" is the same answer as the usage being absent,
// and is honest about what is known.
func TestParseTraceRecordSurvivesUnreadableAttrs(t *testing.T) {
	cases := []struct {
		name string
		wire string
	}{
		{
			name: "usage is not an object",
			wire: `{"seq":1,"kind":"llm.turn","at":"2026-08-09T12:00:00.5Z",` +
				`"attrs":{"model":"gpt-4o","usage":"lots"},` +
				`"deploymentId":"11111111-1111-1111-1111-111111111111"}`,
		},
		{
			name: "attrs is not an object",
			wire: `{"seq":1,"kind":"llm.turn","at":"2026-08-09T12:00:00.5Z","attrs":[1,2,3],` +
				`"deploymentId":"11111111-1111-1111-1111-111111111111"}`,
		},
		{
			name: "the model is not a string",
			wire: `{"seq":1,"kind":"llm.turn","at":"2026-08-09T12:00:00.5Z",` +
				`"attrs":{"model":42,"usage":{"inputTokens":10}},` +
				`"deploymentId":"11111111-1111-1111-1111-111111111111"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTraceRecord([]byte(tc.wire))
			if err != nil {
				t.Fatalf("a record with unreadable attrs was rejected: %v", err)
			}
			if got.Kind != KindLLMTurn {
				t.Fatalf("kind = %q", got.Kind)
			}
		})
	}
}

// The wire counter is unsigned and the column that holds it is not. Clamping
// rather than wrapping means that if it ever overflowed, ordering would degrade
// at the ceiling instead of inverting.
func TestParseTraceRecordClampsAnOverflowingSequence(t *testing.T) {
	wire := `{"seq":18446744073709551615,"kind":"flow.started","at":"2026-08-09T12:00:00.5Z",` +
		`"deploymentId":"11111111-1111-1111-1111-111111111111"}`

	got := mustParse(t, wire)
	if got.Seq < 0 {
		t.Fatalf("seq = %d, want it clamped rather than wrapped negative", got.Seq)
	}
}
