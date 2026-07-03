# Octo — standalone editor

The local, single-user **Octo** visual editor: it embeds the `@octo/editor`
library and supplies a **local-disk filesystem** (flows are `*.yaml` files) and a
**local runner** (the bundled `octo` binary, via `@octo/run-host`). No
orchestrator, auth, or database — this is the "try it out" build.

## Try it (Docker)

```bash
docker run -p 3000:3000 -v "$PWD:/work" juancavallotti/octo
```

Open <http://localhost:3000>, edit a flow, **Save** (writes a `.yaml` into the
mounted directory), and **Run** it with hot reload. The image is published to
public Docker Hub on every release (multi-arch: amd64 + arm64).

## Develop

From the repo root:

```bash
task dev          # builds the octo runner, then runs this app with RUN enabled
```

`task dev` points `OCTO_FS_DIR` at `./.octo-flows`. Running the app directly
(`pnpm --filter standalone dev`) defaults the flow store to `./flows` and leaves
RUN disabled unless `OCTO_BIN_PATH` is set.

| Variable        | Purpose                                         | Default            |
| --------------- | ----------------------------------------------- | ------------------ |
| `OCTO_FS_DIR`   | Directory the editor reads/writes flow YAML in  | `./flows`          |
| `OCTO_BIN_PATH` | Path to the `octo` binary spawned by RUN        | unset (RUN hidden) |
| `OCTO_RUN_DIR`  | Where rendered run configs are written          | OS temp dir        |

## Resources and dev env

A flow's declared resources (env files and templates under `resources:`) are read
from `OCTO_FS_DIR` alongside the flow YAML — e.g. `.env.dev`,
`templates/welcome.tmpl`. On RUN they are staged into the run directory so the
runtime resolves them.

The editor's **Dev .env** panel edits the values for a flow's declared environment
variables. They are stored as a shared `OCTO_FS_DIR/.env.dev` file (not in the
browser), so RUN — and the MCP server — read them from disk. One `.env.dev` is
shared across all flows in the directory.
