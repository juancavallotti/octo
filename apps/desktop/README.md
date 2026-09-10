# Octo Desktop

The standalone editor as a native app that opens a working folder — pick a
directory, edit the flows in it, run them, and point an agent at the MCP endpoint
it advertises. No Docker, no checkout, no terminal.

```bash
task desktop:dev     # run it from the repo, rebuilding on change
task desktop:dist    # package a macOS .app + dmg into dist/release
```

## What it actually is

An Electron shell around the **existing** `apps/standalone` Next server. This app
builds nothing of the editor: it stages the already-built standalone output and
the already-built `octo`/`dolphin` binaries, spawns the server as a child on
Electron's own Node (`ELECTRON_RUN_AS_NODE`), and loads
`http://127.0.0.1:8477` in a window.

That equivalence is the design. There is no desktop build of the editor to keep
in step, the environment contract is the Docker image's verbatim
(`OCTO_FS_DIR`, `OCTO_BIN_PATH`, `DOLPHIN_BIN_PATH`, `OCTO_RUN_DIR`), and a bug
reproduced in `docker run` is the same bug here.

```
Electron main ──spawns──> apps/standalone server.js ──spawns──> octo / dolphin
     │                            │
   window ──loads──> http://127.0.0.1:8477
```

## The MCP endpoint

The editor's own `/mcp` route, unauthenticated and local-only, exactly as in the
Docker image. Its URL is on the app menu (**Octo → Copy MCP Endpoint URL**) and
is written into the open folder as `.octo/mcp-endpoint.json`, so an agent working
in that directory can find it without knowing this app exists.

The port is deliberately fixed at 8477 — clear of the run pools (40000-41999) and
the runtime's observability default (39999) — so an agent configured against it
stays configured. It survives a folder switch, and a crashed instance's orphaned
server is reaped at startup rather than walked past. Pin a different one with
`OCTO_DESKTOP_PORT`.

## One folder at a time

Switching folders restarts the server rather than repointing it. `fsRoot()` reads
`OCTO_FS_DIR` per call, so repointing looks tempting and is wrong: the server
holds state that is not keyed by vault — run sessions, the port pool, staged
resources, the schema cache, SSE subscribers — and repointing would leave runs
from the old folder visible inside the new one. Multiple folders open at once
would mean multiple servers, which would cost the single stable MCP URL.

## Where things live

| | Dev | Packaged |
|---|---|---|
| Editor server | `build/server` (`task desktop:stage`) | `Contents/Resources/server` |
| `octo`, `dolphin` | the repo's `bin/` | `Contents/Resources/bin` |
| State, endpoint record | `~/Library/Application Support/Octo` | same |
| Server log | `~/Library/Logs/Octo/server.log` | same |

`apps/desktop` declares **no runtime dependencies**: main and preload are bundled
by esbuild, and the server and binaries arrive as resources. That is what keeps
electron-builder from having to resolve production deps across pnpm's symlink
farm.

## Shipping

Every release publishes `Octo_mac_arm64.dmg` and `Octo_mac_x64.dmg` to the GitHub
release for the tag (`publish-desktop` in `.github/workflows/release.yml`), built
from the octo and dolphin archives that same release published rather than from a
second compile. The names carry no version so the README and the docs can link to
`releases/latest/download/<name>` and never break.

Signing and notarization are opt-in on the repository secrets being present, so a
fork still gets a green job and an ad-hoc signed build — enough to launch on the
machine that built it, not enough to clear Gatekeeper on a download.

## Not done yet

- **Auto-update.** electron-updater needs the zip target, which is packaged but
  not yet attached to the release.
- **Windows and Linux targets.**
