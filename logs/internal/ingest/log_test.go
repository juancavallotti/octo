package ingest

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseEventExtractsReservedKeysAndKeepsAttrs(t *testing.T) {
	data := []byte(`{
		"time":"2026-06-28T10:11:12.5Z",
		"level":"ERROR",
		"msg":"boom",
		"deploymentId":"dep-1",
		"appName":"checkout",
		"appVersion":"v2",
		"code":42,
		"user":"alice"
	}`)

	ev, err := parseLogEvent(data)
	if err != nil {
		t.Fatalf("parseLogEvent: %v", err)
	}
	if ev.DeploymentID != "dep-1" || ev.AppName != "checkout" || ev.AppVersion != "v2" {
		t.Errorf("identity = %+v, want dep-1/checkout/v2", ev)
	}
	if ev.Level != "ERROR" || ev.Message != "boom" {
		t.Errorf("level/msg = %q/%q, want ERROR/boom", ev.Level, ev.Message)
	}
	want := time.Date(2026, 6, 28, 10, 11, 12, 500_000_000, time.UTC)
	if !ev.Time.Equal(want) {
		t.Errorf("time = %v, want %v", ev.Time, want)
	}

	var attrs map[string]any
	if err := json.Unmarshal(ev.Attrs, &attrs); err != nil {
		t.Fatalf("attrs not valid json: %v", err)
	}
	if attrs["code"] != float64(42) || attrs["user"] != "alice" {
		t.Errorf("attrs = %v, want code=42 user=alice", attrs)
	}
	for _, reserved := range []string{keyTime, keyLevel, keyMessage, keyDeployment, keyAppName, keyAppVersion} {
		if _, ok := attrs[reserved]; ok {
			t.Errorf("reserved key %q leaked into attrs", reserved)
		}
	}
}

func TestParseEventRejectsMissingDeployment(t *testing.T) {
	if _, err := parseLogEvent([]byte(`{"msg":"x"}`)); err == nil {
		t.Fatal("expected an error for a record without a deployment id")
	}
}

func TestParseEventRejectsBadJSON(t *testing.T) {
	if _, err := parseLogEvent([]byte(`not json`)); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

func TestParseEventStampsTimeWhenAbsent(t *testing.T) {
	ev, err := parseLogEvent([]byte(`{"deploymentId":"d","msg":"x"}`))
	if err != nil {
		t.Fatalf("parseLogEvent: %v", err)
	}
	if ev.Time.IsZero() {
		t.Error("expected a stamped time when the record carries none")
	}
}
