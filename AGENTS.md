# Repository Guidance

Before making code changes, read the files under `docs/` and follow them as the source of truth for coding standards, linting, review policy, and release expectations.

The Go module (`github.com/juancavallotti/octo`) is rooted at the repo root; all of its packages live under `runtime/`, with the CLIs in `runtime/octo` (the runtime CLI) and `runtime/dolphin` (its test runner). The Next.js webapps live under
`apps/` as a pnpm workspace: **`apps/platform`** is the orchestrator-backed Octo
app (see [apps/platform/README.md](apps/platform/README.md)). Run `pnpm install`
at the repo root to install all workspace dependencies.

Required reading:

- [docs/coding-standards.md](docs/coding-standards.md) — Go code
- [docs/extension-points.md](docs/extension-points.md) — where new runtime capability goes: the runtime has exactly two extension points (`services` and `connectors`), and adding a third is the failure mode that page prevents
- [docs/editor-coding-standards.md](docs/editor-coding-standards.md) — `apps/` (Next.js) code
- [docs/linting-policy.md](docs/linting-policy.md)
- [docs/commit-and-review-policy.md](docs/commit-and-review-policy.md)
- [docs/release-process.md](docs/release-process.md)

## Workflow rules (always apply)

- Break every implementation plan down into a sequence of small, logical commits.
- **Always stop before committing.** Do not run `git commit` or `git push` until the
  human has reviewed the staged increment and explicitly approved it. Present each
  increment (what changed, why, test coverage) and wait. This applies even to trivial
  changes. See [docs/commit-and-review-policy.md](docs/commit-and-review-policy.md).
- Use Conventional Commit messages — release automation depends on them.

The initial baseline is expected to be committed directly, not through a pull request.

## Documentation policy

The published documentation site lives in **`apps/docs`** (Fumadocs, statically
exported to GitHub Pages). Its content is under `apps/docs/content/docs/`. Keep
it in sync with code **in the same PR** — CI enforces part of this:

- **Connectors, blocks, and sources**: every type registered in the runtime must
  be documented by exactly one page whose `octo_types` frontmatter lists it.
  `scripts/check-docs-drift.mjs` (run in the `validate` workflow and via
  `task docs:check`) compares `bin/octo schema` output against the frontmatter
  and fails on any drift. Adding a connector family ⇒ add
  `apps/docs/content/docs/reference/connectors/<name>.mdx` with an `octo_types`
  list; adding a block ⇒ add its type to the owning reference page's
  `octo_types` and document its settings there.
- **CLI subcommands/flags, CEL variables/functions, flow-file YAML keys**: update
  `reference/cli.mdx`, `reference/cel/`, or `reference/flow-file.mdx` when they
  change. These are not machine-checked — treat a behavior change without a docs
  change as an incomplete PR.
- **New samples** in `samples/` should be linked from `guides/index.mdx` and, when
  they demonstrate a new pattern, get a guide page.
- **Writing style**: second person, present tense, imperative steps; every page
  opens with 1–2 sentences of what/why; no marketing language outside the home
  page; every YAML snippet must use real field names verified against
  `runtime/types/` and prefer adapting `samples/`.
- **Never copy content from `.octo-flows/`** into docs or samples — it is a local
  workspace that may contain live credentials. Re-author with placeholder hosts
  and `env.*` references instead.
- Preview locally with `task docs:dev`; build with `task docs:build`.
- **Publishing**: the site goes to GitHub Pages from the release tag, not from
  main, so it describes the version users can install. A docs change merged today
  appears publicly at the next release (`docs-pages.yml`, called by
  `release-please.yml`). Every PR still builds the docs in `validate.yml`.

## Refactoring policy

This project prefers **complete refactors over backwards compatibility.** When a change
improves the design, update every call site, test, and document in the same change rather
than introducing compatibility shims, deprecated aliases, or dual code paths. There is no
external API stability guarantee yet: prefer one clean, fully-migrated implementation
over preserving old behavior alongside the new.
