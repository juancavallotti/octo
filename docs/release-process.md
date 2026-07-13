# Release Process

## Goals

- Keep the README aligned with the shipped behavior.
- Gate releases on validation, linting, and tests.
- Make release automation visible and repeatable.

## Automation: release-please

Releases are driven by [release-please](https://github.com/googleapis/release-please)
using Conventional Commits.

- Config lives in `release-please-config.json` and `.release-please-manifest.json` at
  the repo root. The repo is released as a single component with clean `vX.Y.Z` tags.
- The `.github/workflows/release-please.yml` workflow runs on every push to `main`.
  It maintains a "release PR" that accumulates the changelog and the next version
  derived from commit messages since the last release.
- Merging the release PR creates the GitHub release, the `CHANGELOG.md` entry, and the
  `vX.Y.Z` tag. The tag push then triggers `.github/workflows/release.yml`
  (`release-check` + `build`).
- Version bumps follow commit types: `feat` → minor, `fix`/`perf`/`refactor` → patch,
  and `!` / `BREAKING CHANGE` → major (pre-1.0, breaking and feature changes bump the
  minor/patch per the config).

## Published artifacts

`.github/workflows/release.yml` gates on `release-check`, then publishes in parallel:

- `publish-cli` — runs GoReleaser (`.goreleaser.yaml`), which cross-compiles both
  CLIs — `octo` and `dolphin` — for macOS, Linux, and Windows (amd64 + arm64 each) and
  attaches `<cli>_<os>_<arch>.tar.gz` / `.zip` plus one combined `checksums.txt` to
  the GitHub release. Both are pure Go, so `CGO_ENABLED=0` builds every target on one
  Linux runner. release-please has already created the release and its notes by then,
  so GoReleaser runs in `keep-existing` mode and only uploads the assets. Two
  invariants in that config are load-bearing and must survive any edit to it:
  archives carry **no version in their filename** (the tag in the release URL
  identifies them, which keeps the README download tables to one version string per
  line for release-please to rewrite), and the checksums file stays named
  **`checksums.txt`** (the README and the install docs link to it by that name).
  Nothing is injected at link time except the build date: the version each binary
  reports is the const release-please stamped into the tagged commit, and `publish-cli`
  fails the release if a const and the tag disagree.
- `publish-standalone` — the standalone editor image, multi-arch, to Docker Hub as
  `juancavallotti/octo`.
- `publish-runtime` — the runtime-only image, multi-arch, to Docker Hub as
  `juancavallotti/octo-runtime`. Same `runtime/Dockerfile` as the cluster image but
  built with `GOTAGS=` (the default, standalone-services build), so it has no
  NATS/Kubernetes dependency.

Cloud Build separately publishes the five platform images and the Helm chart on the
same tag (`cloudbuild.yaml`).

## Expectations

- Validate release readiness before publishing.
- Let release-please own the changelog and version; do not hand-edit `CHANGELOG.md`
  or the manifest version.
- Version strings live behind release-please markers (`extra-files` in
  `release-please-config.json`) — including the README download links. Do not hand-bump
  them, and keep one version per annotated line: the generic updater rewrites a single
  match per line.
- Keep GitHub Actions workflows in sync with the documented process.
