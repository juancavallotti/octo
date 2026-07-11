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

## Manual: editor debugging shots

The `editor-*.png` shots on
[`editor/debugging-flows.mdx`](../apps/docs/content/docs/editor/debugging-flows.mdx)
are captured by hand: they show hover states, open popovers and run results, none
of which the sample harness reproduces. Run `task dev`, open a flow with a couple
of blocks, and crop tightly — these are detail shots, not full-editor frames.

| Screenshot | What it shows |
|---|---|
| `editor-flow-run.png` | A flow card's run menu: *No input*, the saved test inputs, *Add test input*. |
| `editor-add-test-input.png` | The add-test-input form: name, JSON body, variables. |
| `editor-breakpoint-hover.png` | A node's hover cluster — **▶ 🧪 👁 ✕**. Reshoot whenever a button is added or removed. |
| `editor-breakpoint-result.png` | The Output tab, showing the message captured at a breakpoint. |
| `editor-mock-form.png` | A block's 🧪 popover: a case failing with an error when a CEL condition holds, and an *Otherwise…* default returning a canned body. |
| `editor-spy-badge.png` | A block with a spy on it: the 👁 lit, so a watched block is obvious without hovering. |
| `editor-spy-records.png` | The spy popover: the block's address, **Clear**, and the records — a block inside a `foreach`, so there are several, which is the point. |
| `editor-console-tabs.png` | The console header and its tabs. |
| `editor-problems-tab.png` | The Problems tab after a failed run. |

Two things to know before shooting these:

- **A missing PNG breaks the docs build.** MDX imports images at build time, so a
  reference to a file that is not there fails `task docs:build` outright. Use the
  repo's `{/* TODO screenshot: … */}` marker to hold the spot until the image
  exists.
- The `sample-*.png` shots are **not** affected by the node hover buttons: they
  only appear on hover, and no sample carries a mock or a spy.

## Checklist

- [x] Per-sample editor shots (`sample-*.png`) — regenerate after UI changes
- [x] Manual platform/Slack/MCP shots from the table above — placed and wired into their pages
- [x] Editor debugging shots — placed and wired into `editor/debugging-flows.mdx`
