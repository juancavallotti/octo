# Screenshots

Screenshots for the docs site (`apps/docs`) live in
`apps/docs/public/screenshots/` and are referenced from MDX pages as
`/screenshots/<name>.png`. Most are **generated automatically** by a Playwright
harness that renders flows in the real standalone editor. The rest (platform UI,
Slack, MCP clients) need a running backend and are captured manually.

## Auto: per-sample editor shots

`apps/standalone/e2e/screenshots.spec.ts` loads each listed sample at
`/preview?sample=<file>` and captures a full-editor viewport (palette + canvas +
settings panel) into `apps/docs/public/screenshots/sample-<id>.png`.

Regenerate them all:

```bash
cd apps/standalone && npm run screenshots
```

It boots the editor dev server with the OIDC env vars cleared (unauthenticated —
see `auth.config.ts` `authEnabled`), renders each sample, and writes the PNGs at
1440×900 @2x (2880×1800).

To add a sample to the shoot, add it to the `SAMPLES` list in the spec and
reference it from the relevant docs page. Directory samples (`mcp-router/`,
`ai-agent-skills/`) are not supported by the preview loader.

## Manual: backend-dependent shots

These need the platform (orchestrator + SSO) or external apps, so they aren't
automated. Capture at a comparable 16:10 frame, drop the PNG in
`apps/docs/public/screenshots/`, and replace the matching
`{/* TODO screenshot: ... */}` comment in the listed page with a markdown image.

| Screenshot | Docs page |
|---|---|
| Platform dashboard after sign-in | `platform/index.mdx` |
| Integrations list / folders | `platform/integrations.mdx` |
| Deployments view + a rollout in progress | `platform/deployments.mdx` |
| Snapshot / tag history for an integration | `platform/snapshots.mdx` |
| Resources manager + secrets editor (values masked) | `platform/resources-and-secrets.mdx` |
| Object browser (KV store) | `platform/kv-and-storage.mdx` |
| API keys screen (key value masked) | `platform/users-and-api-keys.mdx` |
| Platform log viewer for a running flow | `runtime/monitoring.mdx` |
| Editor settings panel on an `ai-agent` block (tools/skills/memory) | `ai/agents.mdx` |
| Platform canvas of the "Slackbot agent with tools" flow graph | `ai/slack-agent.mdx` |
| Slack thread: mention → "Thinking..." → updated reply | `ai/slack-agent.mdx` |
| MCP client (e.g. Claude) listing `mcp-router` tools | `ai/mcp-server.mdx` |
| MCP client OAuth consent → authorized contacts tool call | `ai/mcp-auth.mdx` |
| Browser rendering the jokes-api / weather-jokes HTML page | `guides/ai-web-app.mdx` |
| Tabbed console: Logs + Dev `.env` + a run's test URL | `editor/running-flows.mdx` |
| OIDC sign-in screen (only if the editor auth page is added) | — |

## Manual: editor detail shots

These `editor-*.png` shots are captured by hand: they show hover states, open
popovers and run results, none of which the sample harness reproduces. Run
`task dev`, open a flow with a couple of blocks, and crop tightly — these are
detail shots, not full-editor frames.

### Debugging

On [`editor/debugging-flows.mdx`](../apps/docs/content/docs/editor/debugging-flows.mdx).

| Screenshot | What it shows |
|---|---|
| `editor-flow-run.png` | A flow card's run menu: *No input*, the saved test inputs, *Add test input*. |
| `editor-add-test-input.png` | The add-test-input form: name, JSON body, variables. |
| `editor-breakpoint-hover.png` | A node's hover cluster — **▶ 🧪 👁 ✕**. Reshoot whenever a button is added or removed. |
| `editor-breakpoint-result.png` | The Output tab, showing the message captured at a breakpoint. |
| `editor-mock-form.png` | A block's 🧪 popover: a case failing with an error when a CEL condition holds, and an *Otherwise…* default returning a canned body. |
| `editor-spy-badge.png` | A block with a spy on it: the 👁 lit, so a watched block is obvious without hovering. |
| `editor-spy-records.png` | The spy popover: the block's address, **Clear**, and the records — a block inside a `foreach`, so there are several, which is the point. |
| `editor-console-tabs.png` | The console header and its five tabs, with a badge on the tab that has something to report. Reshoot whenever a tab is added or removed. |
| `editor-problems-tab.png` | The Problems tab after a failed run. |

### Testing

On the [`testing/`](../apps/docs/content/docs/testing/) section. The Testing tab
is the least visual-friendly part of the docs to describe in prose and the most
intimidating to a newcomer, so it gets the most shots.

#### The capture workspace

Do **not** shoot against the repo's own `.octo-flows/` — it accumulates scratch
flows named `untitled-integration-N.yaml`, and they show up in the Open menu and
in the frame. Swap it aside instead:

```bash
task build                      # bin/octo and bin/dolphin

mv .octo-flows .octo-flows.bak && mkdir .octo-flows
cp samples/error-handling.yaml .octo-flows/
cp samples/error-handling_test.yaml .octo-flows/charge-flowlevel_test.yaml

task dev                        # sets OCTO_PATH, OCTO_BIN_PATH and DOLPHIN_BIN_PATH for you
```

Restore afterwards with `rm -rf .octo-flows && mv .octo-flows.bak .octo-flows`.

Swap the *directory* rather than export `OCTO_FS_DIR`: the root `Taskfile.yml`
sets it in the task's own `env:` block, which wins over the calling shell.

A suite binds to a flow by its `flow:` key, not its filename (`flowOfSuite` in
`apps/standalone/app/api/fs/testSuiteStore.ts`), so a sample suite can be copied
into the workspace under any name and the tab will attach it to the right flow.

#### Session 1 — `error-handling.yaml`

Open it, then pick **Testing** in the view switcher. **Take the first two shots
before pressing *Add tests*** — the no-suite states cannot be recovered after.

| Screenshot | What it shows |
|---|---|
| `editor-testing-tab.png` | The whole tab: the flow rail, the case list, the case form. Full-editor frame, console collapsed. `charge-inline` shows a dimmed flask (no suite), `charge-flowlevel` its case count. Reshoot when `ViewModeToggle` or the three-column layout changes. |
| `editor-testing-empty.png` | A flow with no suite: *Add tests* and the name of the file it will write. Keep the rail in frame, so the contrast with the flow that *has* a suite reads. |
| `editor-testing-scaffold-yaml.png` | A new suite in the YAML view — the commented scaffold from `packages/editor/src/app/suite/scaffold.ts`, scrolled so `flow:`, the commented `env:`/`mocks:` and `cases:` are all visible. |
| `editor-testing-case-form.png` | A case field by field: Name, Input (Shared), Expect (Message), the *subset* hint on Variables, and two CEL rows under That. **No Try button** — this shot is the receipt that it is gone. |
| `editor-testing-mocks.png` | A mock spec: a *When…* case failing with `card declined` above an *Otherwise…* default returning the captured charge, with the `Mock a block…` picker below. |
| `editor-testing-run-results.png` | The console Tests tab, green: the suite's group header, its tally, both cases with elapsed times. Crop the console only, like `editor-console.png`. |
| `editor-testing-failure.png` | A failing case, auto-expanded: want-vs-got beside what the flow actually returned. Produce it by changing the expected `httpStatus` from 502 to 500 — **and revert afterwards**. |
| `editor-testing-run-all.png` | The whole tab after a run-everything, with both suites' groups in the console. |
| `editor-testing-run-all-results.png` | Two suite groups in one report, each with its own tally. |
| `editor-testing-suite-issues.png` | `dolphin will not load this suite`, naming the case at fault, with **Run tests** disabled. (An *unknown key* such as `spys:` would additionally grey out the Form segment — that variant is not shot.) |
| `editor-testing-scenarios-menu.png` | The ▶ menu's *Scenarios* section, listing the suite's cases beside the saved test inputs. |
| `editor-testing-save-as-case.png` | A run's result on the Output tab, with the **Save as test case** button beside it. (The promote popover itself — name, checkboxes, YAML preview — is not shot.) |

Take `editor-console-tabs.png` (the reshoot listed above) while you are here — it is
the shot that goes stale every time a console tab is added.

Two shots came out differently from the brief and the pages were written to match,
so keep this in mind if you reshoot them: `editor-testing-tab.png` is the tab with
no flow selected (not the populated three-column layout), and
`editor-testing-case-form.png` is a freshly scaffolded case, so its fields show
placeholders rather than values. Both work — the first reads as "here is the tab",
the second shows every field without a worked example distracting from it — but
their alt text describes what is actually pictured.

#### Session 2 — `builtins-demo.yaml`, for spies

```bash
cp samples/builtins-demo.yaml .octo-flows/
cp samples/builtins-demo_test.yaml .octo-flows/demo_test.yaml
```

| Screenshot | What it shows |
|---|---|
| `editor-testing-spies.png` | A spy on a block inside a `foreach`: the bracketed address, two crossings asserted, and a CEL expression per crossing. |
| `editor-testing-spy-count-zero.png` | Crossings at `0`, with the hint that says it asserts the block never ran. Very tight crop. |
| `editor-testing-skipped-banner.png` | *N suites were not run*, each with its reason. Free while the workspace holds suites for flows that are not in the open document: just press the header **Run tests**. |

#### Session 3 — `db-orders.yaml`, for suite settings

```bash
cp samples/db-orders.yaml .octo-flows/
cp samples/db-orders_test.yaml .octo-flows/orders_test.yaml
```

| Screenshot | What it shows |
|---|---|
| `editor-testing-suite-settings.png` | Suite settings with all four sections populated — flow under test, timeout, `DB_DSN` as an in-memory database, shared inputs, file-level mocks. The only sample suite that fills every one. |

#### Nice to have

| Screenshot | What it shows |
|---|---|
| `editor-testing-comment-banner.png` | The comment-loss warning, which appears the moment a commented suite is opened in Form view. |
| `editor-testing-no-runner.png` | **Run tests** disabled: `No test runner: DOLPHIN_BIN_PATH is not set on the server.` Relaunch without the variable to get it. |

Five things to know before shooting any of these:

- **A missing PNG breaks the docs build.** MDX imports images at build time, so a
  reference to a file that is not there fails `task docs:build` outright. Use the
  repo's `{/* TODO screenshot: … */}` marker to hold the spot until the image
  exists.
- **Tooltips need a timed capture.** A native `title` tooltip disappears the
  moment you move the mouse toward a crop tool. Use Cmd-Shift-5 → Options →
  5-second timer → Capture Entire Screen, hover, wait, and crop the PNG after.
- **Shoot light.** The `sample-*` family and the detail shots are light-theme;
  `editor-console-tabs.png` and `editor-problems-tab.png` were shot dark and are
  the odd ones out.
- **No terminal screenshots.** dolphin's console reporter emits no ANSI colour at
  all (`runtime/dolphin/internal/report/console.go` is plain `fmt.Fprintf`), so a
  picture of its output is strictly worse than the code block already on the page:
  a code block is copyable, searchable, theme-aware, and cannot break the build by
  going missing. Terminal output stays in MDX.
- **The platform's Testing tab is deliberately not shot.** It is the same
  `@octo/editor` component tree behind an orchestrator, so a shot would cost a
  cluster and teach nothing the standalone frames do not.

The `sample-*.png` shots are **not** affected by the node hover buttons: they only
appear on hover, and no sample carries a mock or a spy.

## Checklist

- [x] Per-sample editor shots (`sample-*.png`) — regenerate after UI changes
- [x] Manual platform/Slack/MCP shots from the table above — placed and wired into their pages
- [x] Editor debugging shots — placed and wired into `editor/debugging-flows.mdx`
- [x] Editor testing shots — placed and wired into the `testing/` section
