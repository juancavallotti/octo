# Screenshot shoot list

The landing page ([index.html](index.html)) carries styled placeholder boxes
(`<div class="shot-placeholder" data-shot="…">`) wherever a screenshot belongs.
This file is the checklist for producing those images.

When an image is ready, drop the PNG in `assets/screenshots/<id>.png` and replace the
placeholder `<div>` in `index.html` with:

```html
<img class="shot" src="assets/screenshots/<id>.png" alt="…" />
```

(The `.shot` class is already styled in [assets/styles.css](assets/styles.css).)

**Target format:** ~16:9, ≥1600px wide, PNG. The Playwright harness renders at 1600×900
with `deviceScaleFactor: 2` (effective 3200×1800) so the "auto" shots come out crisp.

## How the "auto" shots are produced

Shots #1–#3 are captured by the Playwright harness in `editor/` (Increment 2). From the
editor directory:

```bash
npm run screenshots
```

It starts the editor dev server **without** the OIDC env vars (so it's unauthenticated),
loads each sample via `/preview?sample=<name>`, waits for the canvas, and writes the PNGs
straight into `docs/assets/screenshots/`.

## Shots

| # | `data-shot` id        | File                        | Source  | What to capture |
|---|-----------------------|-----------------------------|---------|-----------------|
| 1 | `01-flow-canvas`      | `01-flow-canvas.png`        | auto    | The `ai-router` sample on the editor canvas — the router block with its named routes + default path. Referenced in **What's New**. |
| 2 | `02-error-flow`       | `02-error-flow.png`         | auto    | The `error-handling` sample — a flow showing the `error:` recovery chain. Referenced in **What's New**. |
| 3 | `03-flow-overview`    | `03-flow-overview.png`      | auto    | A representative flow (e.g. `http-orders`) for a hero/overview shot of the canvas. (Spare — wire in where useful.) |
| 4 | `04-block-palette`    | `04-block-palette.png`      | manual  | The component palette open, scrolled to the LLM connectors + AI blocks (`ai-router`, `ai-agent`, `ai-mapping`, `ai-retry`). |
| 5 | `05-ai-settings`      | `05-ai-settings.png`        | manual  | An AI block's settings panel — the `route-list` (ai-router) or `tool-list` (ai-agent) editor with an `inputSchema`. Select the block on the canvas to open it. |
| 6 | `06-oidc-signin`      | `06-oidc-signin.png`        | manual  | The OIDC SSO sign-in screen (`/auth/signin`). Requires running the editor with the `AUTH_EETR_*` env vars set. Referenced in **What's New**. |
| 7 | `07-tabbed-console`   | `07-tabbed-console.png`     | manual  | The bottom console showing the **Logs** / **Dev .env** tabs, with a run's test URL visible in the log panel. Referenced in **What's New**. |

## Manual shots — notes

- **#4 / #5** need an AI sample loaded with the palette/settings open. The harness in
  `editor/` can be extended to click a block and screenshot the settings panel if you'd
  rather automate these.
- **#6** is the only shot that needs auth wired up — run the editor with
  `AUTH_EETR_ISSUER`, `AUTH_EETR_CLIENT_ID`, `AUTH_EETR_CLIENT_SECRET`, and `AUTH_SECRET`
  set, then visit a protected route to be redirected to `/auth/signin`.
- **#7** needs a flow actually running (the test URL only appears once a run starts), so
  it needs `OCTO_BIN_PATH` pointed at a built `octo` binary.

## Checklist

- [x] 1. `01-flow-canvas.png` (auto) — generated + placed in What's New
- [x] 2. `02-error-flow.png` (auto) — generated + placed in What's New
- [x] 3. `03-flow-overview.png` (auto) — generated (spare; not yet placed)
- [ ] 4. `04-block-palette.png` (manual)
- [ ] 5. `05-ai-settings.png` (manual)
- [ ] 6. `06-oidc-signin.png` (manual)
- [ ] 7. `07-tabbed-console.png` (manual)
