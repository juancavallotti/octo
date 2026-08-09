package k8s

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// The wire format is the contract with a traces engine that lives in another Go
// module and cannot import this one, so it is pinned here rather than left to
// whatever the struct happens to marshal to. These tests cover the encoding only;
// the publish path needs a broker and is verified by running one.

func TestTraceRecordFlattensTheDeploymentIdentity(t *testing.T) {
	sink := newTraceSink(nil, "dep-1", "orders", "v3")

	raw, err := json.Marshal(traceRecord{
		TraceEvent: types.TraceEvent{
			Seq:        7,
			Kind:       types.TraceBlockPostInvoke,
			TraceID:    "trace-1",
			EventID:    "event-1",
			Flow:       "orders",
			Path:       "orders.charge",
			BlockType:  "rest",
			OccurredAt: time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
			DurationNs: 1500,
		},
		DeploymentID: sink.deploymentID,
		AppName:      sink.appName,
		AppVersion:   sink.appVersion,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The deployment fields sit beside the record's own, not inside an envelope:
	// one flat object is what the log records already arrive in, and what a
	// consumer can denormalize into columns without unwrapping anything.
	for field, want := range map[string]any{
		"deploymentId": "dep-1",
		"appName":      "orders",
		"appVersion":   "v3",
		"kind":         string(types.TraceBlockPostInvoke),
		"traceId":      "trace-1",
		"eventId":      "event-1",
		"flow":         "orders",
		"path":         "orders.charge",
		"blockType":    "rest",
	} {
		if decoded[field] != want {
			t.Errorf("%s = %v, want %v", field, decoded[field], want)
		}
	}
	if decoded["seq"] != float64(7) {
		t.Errorf("seq = %v, want 7", decoded["seq"])
	}
	if decoded["durationNs"] != float64(1500) {
		t.Errorf("durationNs = %v, want 1500", decoded["durationNs"])
	}
}

// An unnamed or untagged deployment still publishes; only the id is guaranteed,
// and it is what the consumer keys on.
func TestTraceRecordOmitsAnEmptyAppIdentity(t *testing.T) {
	raw, err := json.Marshal(traceRecord{
		TraceEvent:   types.TraceEvent{Kind: types.TraceFlowStarted},
		DeploymentID: "dep-1",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["appName"]; ok {
		t.Error("an unnamed deployment emitted an appName field")
	}
	if decoded["deploymentId"] != "dep-1" {
		t.Errorf("deploymentId = %v, want it always present", decoded["deploymentId"])
	}
}

// Tracing off means no publisher and no subscription to a broker that may not
// even be reachable — the module must not do setup nobody asked for.
func TestTracingDisabledYieldsTheNoopPublisher(t *testing.T) {
	publisher := newTracePublisher(nil, core.TraceOptions{Enabled: false}, "dep-1", "orders", "v3")
	if publisher.Enabled() {
		t.Fatal("a disabled module returned an enabled publisher")
	}
	// Publishing to it must not touch the nil connection.
	publisher.Publish(types.TraceEvent{Kind: types.TraceFlowStarted})
}
