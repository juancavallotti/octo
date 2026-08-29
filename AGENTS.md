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
- **Two gates: the plan is approved before the work starts, and `git push` /
  opening a pull request is approved.** In between, chain the agreed sequence. A
  commit that changes an architectural contract, or one you are not confident
  about, still stops. See
  [docs/commit-and-review-policy.md](docs/commit-and-review-policy.md) — which is
  the policy; this is a pointer to it, not a second copy.
- Use Conventional Commit messages — release automation depends on them.

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

## Layering policy

**Every layer owns the problem it creates.** A problem created higher up the stack
is never solved lower down.

If a flow's `input` expression fabricates a prompt blob, the surfaces belonging to
that flow render it back down — not the runtime, not the store, not a generic
platform view. If an integration needs its own shape handled, the handling lives
with that integration. The runtime, the orchestrator and the platform UI are shared
by everything, and must not learn any one implementation's quirks.

Two rules follow, and both are absolute:

- **Do not push a concern down the stack to make a fix easier.** The tell is
  needing *new shared vocabulary* — a new block setting, a new store field, a new
  generic flag — to express one caller's shape. When a fix requires that, it is at
  the wrong layer. Stop and put it where the shape originates.
- **Do not promote something to the platform on your own judgement.** If a
  capability genuinely belongs in a shared layer, the repository owner will ask for
  it to be added there. Proposing it is fine; landing it unasked is not.

The corollary for stored data: **anything durable is written as it was produced and
read back as it was written.** Agent memory records the turn an agent sent,
verbatim. Later readers cannot be anticipated — audit, replay, reconstructing what
a model actually saw — so trimming at write time serves one reader by destroying
the record for every other. Reshaping happens at the edge, on read, by the surface
that knows why.

## Refactoring policy

This project prefers **complete refactors over backwards compatibility.** When a change
improves the design, update every call site, test, and document in the same change rather
than introducing compatibility shims, deprecated aliases, or dual code paths. There is no
external API stability guarantee yet: prefer one clean, fully-migrated implementation
over preserving old behavior alongside the new.
