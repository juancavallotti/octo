package main

import (
	"fmt"
	"runtime/debug"
)

// Version is the octo release version. release-please keeps this in sync with
// the published release via the extra-files updater in release-please-config.json
// (the trailing annotation marks the line it rewrites). See docs/index.html for
// the matching marker on the website.
const Version = "0.1.7" // x-release-please-version

// BuildDate is the binary's build timestamp, stamped at link time via
// -ldflags "-X main.BuildDate=...". The build task sets it for released and
// `task build` binaries; when unset (e.g. `go run`) versionLine falls back to the
// VCS commit time embedded by `go build`, and omits the date if neither exists.
var BuildDate string

// versionLine returns the `--version` output: the program name and version, plus
// a "(built <timestamp>)" suffix when a build date is available.
func versionLine() string {
	if date := buildDate(); date != "" {
		return fmt.Sprintf("octo %s (built %s)", Version, date)
	}
	return fmt.Sprintf("octo %s", Version)
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

// readyBanner is the friendly line printed to stdout once the runtime has started
// every connector and flow and is accepting traffic.
func readyBanner() string {
	return fmt.Sprintf("🚀  octo v%s is up and ready to roll!", Version)
}
