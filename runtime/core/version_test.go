package core

import (
	"regexp"
	"testing"
)

// semver matches the shape release-please writes. It is anchored at BOTH ends:
// this value is reported as an MCP serverInfo.version, so "0.8.1-wip" or a
// half-rewritten "0.8.1x-release-please" has to fail rather than ship.
var semver = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// TestVersionIsAReleaseVersion checks that Version looks like one. A botched
// release-please rewrite would otherwise ship silently and surface as an
// mcp-router advertising "x-release-please-version" as its server version.
func TestVersionIsAReleaseVersion(t *testing.T) {
	if Version == "" {
		t.Fatal("Version is empty")
	}
	if !semver.MatchString(Version) {
		t.Fatalf("Version %q does not start with a major.minor.patch version", Version)
	}
}
