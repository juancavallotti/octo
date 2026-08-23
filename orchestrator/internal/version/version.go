// Package version carries the octo release this orchestrator was built from.
//
// It is a package of its own, and not a constant beside its one caller, because
// the orchestrator is a separate Go module: it cannot reuse runtime/core's copy,
// and its own root is package main, which nothing can import. So the release has
// to live somewhere both the embedded agent and anything that reports what this
// binary is can reach.
//
// release-please keeps this in sync with the published release via the extra-files
// updater in release-please-config.json; the trailing annotation marks the line it
// rewrites. Every module carries its own marked copy of the same literal, which is
// why the annotation appears more than once in the tree.
package version

const Version = "0.8.5" // x-release-please-version
