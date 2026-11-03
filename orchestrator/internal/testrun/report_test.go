package testrun

import (
	"encoding/json"
	"errors"
	"os"
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

// The fixtures are a test pod's real output: the console summary, the report on
// stdout, and — for the failing run — dolphin's exit message on stderr after it.
func TestExtractReportFromPodOutput(t *testing.T) {
	cases := []struct {
		file           string
		passed, failed int
	}{
		{"testdata/passed.txt", 3, 0},
		{"testdata/failed.txt", 2, 1},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			logs, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			got, err := extractReport(logs)
			if err != nil {
				t.Fatalf("extractReport: %v", err)
			}
			var doc struct {
				Totals struct{ Passed, Failed int } `json:"totals"`
				Suites []json.RawMessage            `json:"suites"`
			}
			if err := json.Unmarshal(got, &doc); err != nil {
				t.Fatal(err)
			}
			if doc.Totals.Passed != tc.passed || doc.Totals.Failed != tc.failed || len(doc.Suites) != 1 {
				t.Errorf("totals = %+v, suites %d", doc.Totals, len(doc.Suites))
			}
		})
	}
}
