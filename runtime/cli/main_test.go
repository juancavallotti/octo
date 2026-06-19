package main

import (
	"strings"
	"testing"
)

// TestRunVersion checks that every version invocation form is handled in run()
// and returns nil without falling through to the run/invoke flagsets (which do
// not define the flag and would otherwise error).
func TestRunVersion(t *testing.T) {
	for _, args := range [][]string{
		{"--version"},
		{"-version"},
		{"version"},
	} {
		if err := run(args); err != nil {
			t.Errorf("run(%q) = %v, want nil", args, err)
		}
	}
}

// TestVersionLine checks the python/java-style output: name and version always,
// with a "(built ...)" suffix only when a build date is present.
func TestVersionLine(t *testing.T) {
	orig := BuildDate
	t.Cleanup(func() { BuildDate = orig })

	BuildDate = ""
	if got := versionLine(); !strings.HasPrefix(got, "octo "+Version) {
		t.Errorf("versionLine() = %q, want prefix %q", got, "octo "+Version)
	}

	BuildDate = "2026-06-18T22:31:23Z"
	got := versionLine()
	if !strings.HasPrefix(got, "octo "+Version) {
		t.Errorf("versionLine() = %q, want prefix %q", got, "octo "+Version)
	}
	if !strings.Contains(got, "(built "+BuildDate+")") {
		t.Errorf("versionLine() = %q, want build date %q rendered", got, BuildDate)
	}
}
