package podstats

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The pod stats wire contract, checked against the other side of it.
//
// Entry, Sample and Bucket are duplicated from sidecars/stats, a module that
// cannot import this one and that this one cannot import. Drift between the
// copies does not fail to compile and does not fail any test that exercises a
// single side: a renamed JSON tag on the writer means this decoder reads a zero
// where a reading was, and a chart quietly shows a flat line instead of an
// error.
//
// So this reads the writer's source and compares the field-to-tag mappings
// themselves. Hashing each copy against a pinned constant would be weaker in
// exactly the case that matters — change a struct and update the constant
// beside it, and that side stays green while the other drifts — which is the
// reasoning runtime/services/k8s/rediskv_contract_test.go already settled for
// the volatile KV layout.
//
// It lives on the reading side because this is the side that breaks silently.
// The writer notices a format it cannot produce; a reader cannot notice a
// number that decoded to the wrong thing.

// The writer's files, relative to this package's directory.
const (
	writerSeries = "../../../sidecars/stats/internal/series/series.go"
	writerRollup = "../../../sidecars/stats/internal/rollup/rollup.go"
	writerStore  = "../../../sidecars/stats/internal/store/store.go"
	writerValues = "../../../sidecars/stats/internal/series/values.go"
)

// structDecl captures a named struct's body, non-greedily to the first closing
// brace at column zero.
var structDecl = regexp.MustCompile(`(?ms)^type (\w+) struct \{\n(.*?)^\}`)

// taggedField captures a field name and its json tag from one declaration line.
//
// Anchored per line on purpose: without it the type portion matches across
// newlines and comments, and a field pairs with an earlier field's name. The
// Go type is deliberately not compared — the two sides differ there, because
// the writer needs an encoder and this side only a decoder — but the tag,
// including any ",omitempty", is the contract.
var taggedField = regexp.MustCompile("(?m)^\\s*(\\w+)\\s+[^`\\n]+`json:\"([^\"]+)\"`")

// layoutConst captures the Layout string both sides declare.
var layoutConst = regexp.MustCompile(`Layout = "([^"]+)"`)

// TestWireStructsMatchTheWriter compares the JSON shape of every duplicated
// struct against the declaration it was copied from.
func TestWireStructsMatchTheWriter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{"Entry", writerSeries},
		{"Sample", writerSeries},
		{"Bucket", writerRollup},
	} {
		t.Run(tc.name, func(t *testing.T) {
			theirs, ok := jsonShape(t, tc.source, tc.name)
			if !ok {
				t.Fatalf("no struct %s in %s; the writer moved it, and this "+
					"contract no longer checks anything", tc.name, tc.source)
			}
			ours, ok := jsonShape(t, thisFile(t, tc.name), tc.name)
			if !ok {
				t.Fatalf("no struct %s in this package", tc.name)
			}

			if ours != theirs {
				t.Errorf("%s disagrees with the writer.\n  writer (%s):\n    %s\n"+
					"  here:\n    %s\n\n"+
					"These two are hand-synced across module boundaries. Whichever "+
					"side moved, move the other to match.",
					tc.name, tc.source, theirs, ours)
			}
		})
	}
}

// TestLayoutMatchesTheWriter checks the key shape, which is the other half of
// the contract: identical structs read out of the wrong keys are still wrong.
func TestLayoutMatchesTheWriter(t *testing.T) {
	source, ok := readWriter(t, writerStore)
	if !ok {
		return
	}

	match := layoutConst.FindStringSubmatch(source)
	if match == nil {
		t.Fatalf("no Layout constant in %s", writerStore)
	}
	if match[1] != Layout {
		t.Errorf("key layout disagrees with the writer.\n  writer: %s\n  here:   %s",
			match[1], Layout)
	}
}

// TestWriterEncodesGapsAsNull pins the one semantic this decoder cannot verify
// structurally: that a non-finite reading is written as null rather than as a
// number. If the writer went back to a plain []float64, Values.UnmarshalJSON
// here would still compile and still decode — it would just never see a gap,
// because the writer would be failing to store those rows at all.
func TestWriterEncodesGapsAsNull(t *testing.T) {
	source, ok := readWriter(t, writerValues)
	if !ok {
		return
	}
	if !strings.Contains(source, "func (v Values) MarshalJSON()") {
		t.Error("the writer no longer defines Values.MarshalJSON; gaps may not " +
			"be reaching Redis as null, which is what this decoder assumes")
	}
}

// jsonShape renders a struct's field-to-tag mapping as one sorted, comparable
// string. Sorted because field order is not part of the JSON contract, and
// reordering fields should not fail this test.
func jsonShape(t *testing.T, path, name string) (string, bool) {
	t.Helper()

	source, ok := readWriter(t, path)
	if !ok {
		// readWriter skipped the test; unreachable, but keeps the signature
		// honest for callers that pass a path inside this package.
		return "", false
	}

	for _, decl := range structDecl.FindAllStringSubmatch(source, -1) {
		if decl[1] != name {
			continue
		}
		var pairs []string
		for _, field := range taggedField.FindAllStringSubmatch(decl[2], -1) {
			pairs = append(pairs, fmt.Sprintf("%s=%s", field[1], field[2]))
		}
		sort.Strings(pairs)
		return strings.Join(pairs, " "), true
	}
	return "", false
}

// thisFile names the file in this package holding a given struct, so the same
// extraction runs over both sides rather than trusting reflection on one and
// parsing on the other. Reflection would report this package's own tags
// faithfully and tell us nothing about whether they were compared correctly.
func thisFile(t *testing.T, name string) string {
	t.Helper()
	switch name {
	case "Entry", "Sample", "Bucket":
		return "wire.go"
	default:
		t.Fatalf("no file recorded for %s", name)
		return ""
	}
}

// readWriter reads one of the writer's files, skipping the test when the
// sidecars module is not present. Each module stays independently consumable,
// which is the same allowance the volatile KV contract test makes.
func readWriter(t *testing.T, path string) (string, bool) {
	t.Helper()

	source, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if strings.HasPrefix(path, "..") {
			t.Skipf("%s is not present; the sidecars module is not checked out "+
				"beside this one", path)
			return "", false
		}
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(source), true
}
