package main

import (
	"strings"
	"testing"
)

// TestFlagsWorkOnEitherSideOfThePaths is a regression test for a bug that would have been
// very hard to notice.
//
// Go's flag package STOPS parsing at the first non-flag argument. So in
//
//	dolphin test ./flows --junit report.xml
//
// --junit is never parsed — and the naive fix (filter out anything starting with a dash
// and treat the rest as paths) makes it worse: the flag is silently dropped and
// `report.xml` becomes a path to go looking for suites in. A CI job would get no report,
// and nothing anywhere would say why.
//
// So each non-flag argument is taken as a path and parsing resumes after it, which is
// what people will type because it is what `go test` accepts.
func TestFlagsWorkOnEitherSideOfThePaths(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "flags first", args: []string{"--junit", "r.xml", "--report-json", "r.json", "--parallel", "2", "-v", "./flows", "./more"}},
		{name: "flags last", args: []string{"./flows", "./more", "--junit", "r.xml", "--report-json", "r.json", "--parallel", "2", "-v"}},
		{name: "flags around", args: []string{"--junit", "r.xml", "./flows", "--report-json", "r.json", "--parallel", "2", "./more", "-v"}},
		{name: "an = form", args: []string{"./flows", "--junit=r.xml", "--report-json=r.json", "--parallel=2", "./more", "-v"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flags, err := parseTestFlags(tc.args)
			if err != nil {
				t.Fatalf("parse %v: %v", tc.args, err)
			}

			if flags.junit != "r.xml" {
				t.Errorf("--junit = %q, want r.xml — the flag was dropped", flags.junit)
			}
			if flags.reportJSON != "r.json" {
				t.Errorf("--report-json = %q, want r.json — the flag was dropped", flags.reportJSON)
			}
			if flags.parallel != 2 {
				t.Errorf("--parallel = %d, want 2", flags.parallel)
			}
			if !flags.verbose {
				t.Error("-v was dropped")
			}
			if got := strings.Join(flags.paths, ","); got != "./flows,./more" {
				t.Errorf("paths = %q, want ./flows,./more", got)
			}
		})
	}
}

// TestNoPathsMeansHere: `dolphin test` on its own tests the current directory, as
// `go test` does.
func TestNoPathsMeansHere(t *testing.T) {
	flags, err := parseTestFlags(nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(flags.paths) != 0 {
		t.Errorf("paths = %v, want none (discovery defaults them to \".\")", flags.paths)
	}
}

// TestAnUnknownFlagIsRejected: it must not be quietly taken for a path.
func TestAnUnknownFlagIsRejected(t *testing.T) {
	misspelled := "--paralell" //nolint:misspell // that is the point: a typo must be rejected, not ignored

	if _, err := parseTestFlags([]string{"./flows", misspelled, "2"}); err == nil {
		t.Fatal("a misspelled flag was accepted")
	}
}
