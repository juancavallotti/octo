package main

import "fmt"

// Version is the eip-go release version. release-please keeps this in sync with
// the published release via the extra-files updater in release-please-config.json
// (the trailing annotation marks the line it rewrites). See docs/index.html for
// the matching marker on the website.
const Version = "0.1.0" // x-release-please-version

// readyBanner is the friendly line printed to stdout once the runtime has started
// every connector and flow and is accepting traffic.
func readyBanner() string {
	return fmt.Sprintf("🚀  octo v%s is up and ready to roll!", Version)
}
