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

## Expectations

- Validate release readiness before publishing.
- Let release-please own the changelog and version; do not hand-edit `CHANGELOG.md`
  or the manifest version.
- Keep GitHub Actions workflows in sync with the documented process.
