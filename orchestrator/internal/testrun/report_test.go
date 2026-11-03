package testrun

import (
	"encoding/json"
	"errors"
	"testing"
)

const report = `{
  "dolphin": "dolphin 0.11.14",
  "wallMs": 12,
  "totals": {"cases": 1, "passed": 0, "failed": 1}
}
`

func TestExtractReport(t *testing.T) {
	t.Run("between the console summary and the exit message", func(t *testing.T) {
		logs := "orders_test.yaml  FAIL  1 case\n  {not json}\n" + report + "dolphin: 1 case(s) failed\n"
		got, err := extractReport([]byte(logs))
		if err != nil {
			t.Fatalf("extractReport: %v", err)
		}
		var doc struct {
			Totals struct{ Failed int } `json:"totals"`
		}
		if err := json.Unmarshal(got, &doc); err != nil || doc.Totals.Failed != 1 {
			t.Errorf("report = %s, err %v", got, err)
		}
	})

	t.Run("compact", func(t *testing.T) {
		if _, err := extractReport([]byte(`{"dolphin":"x","totals":{}}`)); err != nil {
			t.Errorf("extractReport: %v", err)
		}
	})

	t.Run("none", func(t *testing.T) {
		_, err := extractReport([]byte("octo: no such flow\n"))
		if !errors.Is(err, errNoReport) {
			t.Errorf("err = %v, want errNoReport", err)
		}
	})

	t.Run("cut short", func(t *testing.T) {
		_, err := extractReport([]byte(report[:30]))
		if !errors.Is(err, errNoReport) {
			t.Errorf("err = %v, want errNoReport", err)
		}
	})
}
