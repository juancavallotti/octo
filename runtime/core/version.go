package core

// Version is the octo release version as the *runtime* knows it, for the parts
// of the runtime that have to report what they are.
//
// It is declared here because the binary's own version lives in package main,
// which nothing in the runtime can import.
//
// release-please keeps this in sync with the published release via the
// extra-files updater in release-please-config.json; the trailing annotation
// marks the line it rewrites. The CLIs carry their own marked copies of the same
// literal, which is why the annotation appears more than once in the tree.
const Version = "0.11.6" // x-release-please-version
