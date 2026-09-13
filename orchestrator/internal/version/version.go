// Package version carries the octo release this orchestrator was built from.
//
// A package of its own, and not a constant beside its one caller, because this
// module's root is package main and nothing can import it.
//
// release-please keeps the literal in sync with the published release via the
// extra-files updater in release-please-config.json; the trailing annotation marks
// the line it rewrites.
package version

const Version = "0.11.3" // x-release-please-version
