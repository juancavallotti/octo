package main

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// Version is the octo release version. release-please keeps this in sync with
// the published release via the extra-files updater in release-please-config.json
// (the trailing annotation marks the line it rewrites). See docs/index.html for
// the matching marker on the website.
const Version = "0.9.1" // x-release-please-version

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

// readyBanner is the friendly block printed to stdout once the runtime has started
// every connector and flow and is accepting traffic. It frames the greeting so it
// stands out from the structured log lines around it, and says what is serving.
func readyBanner(flows, connectors int) string {
	lines := []string{
		fmt.Sprintf("🐙  octo v%s is up and ready to roll!  🐙", Version),
		fmt.Sprintf("%s · %s serving", count(flows, "flow"), count(connectors, "connector")),
	}
	inner := 0
	for _, line := range lines {
		inner = max(inner, displayWidth(line))
	}
	inner += 4 // two columns of padding either side of the widest line

	var b strings.Builder
	b.WriteString("╭" + strings.Repeat("─", inner) + "╮\n")
	for _, line := range lines {
		// Centre each line in the box, giving the odd column to the right side.
		space := inner - displayWidth(line)
		b.WriteString("│" + strings.Repeat(" ", space/2) + line + strings.Repeat(" ", space-space/2) + "│\n")
	}
	b.WriteString("╰" + strings.Repeat("─", inner) + "╯")
	return b.String()
}

// count renders a quantity with its noun, pluralised the regular way — every noun
// the banner names is regular.
func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// lastBMP is the final code point of the Basic Multilingual Plane. Everything the
// banner carries above it is emoji, which is what makes it a width test.
const lastBMP = 0xFFFF

// displayWidth is the number of terminal columns a banner line occupies. The runes
// above the BMP here are emoji, which terminals draw double-width; everything else
// in the banner is ASCII or box drawing, which are single-width.
func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		if r > lastBMP {
			width += 2
			continue
		}
		width++
	}
	return width
}
