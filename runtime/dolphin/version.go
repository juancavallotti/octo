package main

import (
	"fmt"
	"runtime/debug"
)

// Version is the dolphin release version. dolphin ships in the same release train
// as octo — one tag, one version — so this const tracks octo's, and release-please
// keeps it in sync via the extra-files updater in release-please-config.json (the
// trailing annotation marks the line it rewrites).
const Version = "0.8.4" // x-release-please-version

// BuildDate is the binary's build timestamp, stamped at link time via
// -ldflags "-X main.BuildDate=...". The build task and the release set it; when
// unset (e.g. `go run`, or `go install`, which cannot pass ldflags) versionLine
// falls back to the VCS commit time embedded by `go build`, and omits the date if
// neither exists.
var BuildDate string

// versionLine returns the `version` output: the program name and version, plus a
// "(built <timestamp>)" suffix when a build date is available.
func versionLine() string {
	if date := buildDate(); date != "" {
		return fmt.Sprintf("dolphin %s (built %s)", Version, date)
	}
	return fmt.Sprintf("dolphin %s", Version)
}

// buildDate resolves the build timestamp: the linker-stamped BuildDate when set,
// otherwise the vcs.time build setting Go embeds into `go build` binaries.
func buildDate() string {
	if BuildDate != "" {
		return BuildDate
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.time" {
			return setting.Value
		}
	}
	return ""
}
