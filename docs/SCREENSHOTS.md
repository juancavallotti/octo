# Screenshots

Most screenshots on the docs site are **generated automatically** by a Playwright
harness in `editor/` that renders flows in the real editor. A few (auth, console,
deployments) need a running backend and are captured manually.

## Auto: per-sample editor shots

`editor/e2e/screenshots.spec.ts` loads every gallery sample at `/preview?sample=<file>`
and captures a full-editor viewport (palette + canvas + settings panel) into
`docs/assets/screenshots/sample-<id>.png`. The samples gallery shows each one inside its
panel (see [assets/app.js](assets/app.js)); the What's New section reuses two of them.

Regenerate them all:

```bash
cd editor && npm run screenshots
```

It boots the editor dev server with the OIDC env vars cleared (so it's unauthenticated —
see `auth.config.ts` `authEnabled`), renders each sample, and writes the PNGs. Output is
1440×900 @2x (2880×1800).

Covered samples (id → `samples/<file>.yaml`): hello-world, http-orders, db-orders,
weather, flow-to-flow (flow-to-flow-http), builtins (builtins-demo), ai-router, ai-agent,
ai-mapping, ai-retry, error-handling, file-logger, heartbeat.

To add a sample to the shoot, add it to the `SAMPLES` list in the spec and to the gallery
in `index.html`.

## Manual: backend-dependent shots

These need the orchestrator / a running flow / SSO configured, so they aren't automated.
They are **not currently placed** on the site — the non-automatic placeholders were removed
for now. When you capture one, drop the PNG in `assets/screenshots/<id>.png` and add an
`<img class="shot" src="assets/screenshots/<id>.png" alt="…" />` where you want it (e.g. in
the What's New section or a future "Product tour").

| `data-shot` id      | What to capture | Notes |
|---------------------|-----------------|-------|
| `06-oidc-signin`    | The OIDC SSO sign-in screen (`/auth/signin`). | Run the editor with `AUTH_EETR_*` + `AUTH_SECRET` set. Referenced in What's New. |
| `07-tabbed-console` | Bottom console: Logs / Dev `.env` tabs + a run's test URL. | Needs `OCTO_BIN_PATH` so a run can start. Referenced in What's New. |
| `08-deployments`    | The deployments management view for an integration. | Needs `ORCHESTRATOR_URL` + a saved integration with deployments. Not yet placed. |
| `09-integrations`   | The integrations list / manager (`/integrations`). | Needs `ORCHESTRATOR_URL`. Not yet placed. |

## Checklist

- [x] Per-sample gallery shots (`sample-*.png`, ×13) — generated + shown in the gallery
- [x] What's New reuses `sample-ai-router.png` + `sample-error-handling.png`
- [ ] `06-oidc-signin.png` (manual)
- [ ] `07-tabbed-console.png` (manual)
- [ ] `08-deployments.png` (manual)
- [ ] `09-integrations.png` (manual)
