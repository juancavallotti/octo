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

## Checklist

- [x] Per-sample editor shots (`sample-*.png`) — regenerate after UI changes
- [x] Manual platform/Slack/MCP shots from the table above — placed and wired into their pages
