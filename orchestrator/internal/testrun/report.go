package testrun

import (
	"bytes"
	"encoding/json"
	"errors"
)

// reportStarts are how dolphin's JSON report begins: indented, as dolphin writes
// it, or compact.
var reportStarts = [][]byte{[]byte("{\n  \"dolphin\""), []byte("{\"dolphin\"")}

// errNoReport is returned when a test pod's output carries no report.
var errNoReport = errors.New("dolphin wrote no report")

// extractReport finds dolphin's JSON report in a test pod's output.
//
// The pod's log interleaves dolphin's console summary, the report it wrote to
// stdout and anything it said on stderr. The report is written in one piece after
// the summary, so it is the last place a report begins, and decoding stops at the
// end of that one object whatever follows it.
func extractReport(logs []byte) (json.RawMessage, error) {
	at := -1
	for _, start := range reportStarts {
		if i := bytes.LastIndex(logs, start); i > at {
			at = i
		}
	}
	if at < 0 {
		return nil, errNoReport
	}
	var ret json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(logs[at:])).Decode(&ret); err != nil {
		return nil, errors.Join(errNoReport, err)
	}
	return ret, nil
}

// maxLogTail bounds how much of a pod's output is kept as a run's error when it
// produced no report.
const maxLogTail = 4096

// logTail is the end of a pod's output, which is where a run that wrote no report
// said why.
func logTail(logs []byte) string {
	if len(logs) > maxLogTail {
		logs = logs[len(logs)-maxLogTail:]
	}
	return string(bytes.TrimSpace(logs))
}
