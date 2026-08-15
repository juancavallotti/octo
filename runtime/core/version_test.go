package core

import (
	"regexp"
	"testing"
)

// semver matches the shape release-please writes. It is deliberately anchored:
// the failure this guards against is the updater not firing at all, which leaves
// the marker text or a placeholder in the constant rather than a version.
var semver = regexp.MustCompile(`^\d+\.\d+\.\d+`)

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
