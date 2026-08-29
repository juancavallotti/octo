package core

// Version is the octo release version as the *runtime* knows it, for the parts
// of the runtime that have to report what they are.
//
// It is declared here rather than reused from the CLI because runtime/octo is
// package main: the engine cannot import it. The mcp-router is the first caller —
// an MCP server's initialize response carries a serverInfo.version, and a
// hardcoded one is a lie in every deployment.
//
// release-please keeps this in sync with the published release via the
// extra-files updater in release-please-config.json; the trailing annotation
// marks the line it rewrites. The CLIs carry their own marked copies of the same
// literal, which is why the annotation appears more than once in the tree.
const Version = "0.8.10" // x-release-please-version
