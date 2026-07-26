# Octo

**Octo** is a cloud-native integration runtime. The `octo` repository holds its
stacks, including a Go module for the runtime engine and CLI.

**Documentation:** the docs site — overview, getting started, connector and CEL
reference, guides, and the AI/MCP story — is built from [`apps/docs/`](apps/docs/)
(Fumadocs, statically exported) and published with GitHub Pages at
<https://juancavallotti.github.io/octo/>. The site is published from each release
tag, so it describes the version you can install — not unreleased `main`. Preview
locally with `task docs:dev`.

## Install

Download a prebuilt CLI — no Go toolchain needed. Unpack it and put the binary on
your PATH. `octo` is the runtime; `dolphin` is its companion test runner, which
drives `octo` to unit-test an integration.

<!-- The download links are rewritten on every release by release-please (see the
     extra-files entry in release-please-config.json). Keep exactly one version
     string per line inside the markers — the generic updater rewrites one match
     per line, which is why the archive names carry no version. -->
<!-- x-release-please-start-version -->

### octo — the runtime

| Platform | Download |
|---|---|
| macOS (Apple Silicon) | [octo_darwin_arm64.tar.gz](https://github.com/juancavallotti/octo/releases/download/v0.5.0/octo_darwin_arm64.tar.gz) |
| macOS (Intel) | [octo_darwin_amd64.tar.gz](https://github.com/juancavallotti/octo/releases/download/v0.5.0/octo_darwin_amd64.tar.gz) |
| Linux (x86-64) | [octo_linux_amd64.tar.gz](https://github.com/juancavallotti/octo/releases/download/v0.5.0/octo_linux_amd64.tar.gz) |
| Linux (arm64) | [octo_linux_arm64.tar.gz](https://github.com/juancavallotti/octo/releases/download/v0.5.0/octo_linux_arm64.tar.gz) |
| Windows (x86-64) | [octo_windows_amd64.zip](https://github.com/juancavallotti/octo/releases/download/v0.5.0/octo_windows_amd64.zip) |
| Windows (arm64) | [octo_windows_arm64.zip](https://github.com/juancavallotti/octo/releases/download/v0.5.0/octo_windows_arm64.zip) |

### dolphin — the test runner

| Platform | Download |
|---|---|
| macOS (Apple Silicon) | [dolphin_darwin_arm64.tar.gz](https://github.com/juancavallotti/octo/releases/download/v0.5.0/dolphin_darwin_arm64.tar.gz) |
| macOS (Intel) | [dolphin_darwin_amd64.tar.gz](https://github.com/juancavallotti/octo/releases/download/v0.5.0/dolphin_darwin_amd64.tar.gz) |
| Linux (x86-64) | [dolphin_linux_amd64.tar.gz](https://github.com/juancavallotti/octo/releases/download/v0.5.0/dolphin_linux_amd64.tar.gz) |
| Linux (arm64) | [dolphin_linux_arm64.tar.gz](https://github.com/juancavallotti/octo/releases/download/v0.5.0/dolphin_linux_arm64.tar.gz) |
| Windows (x86-64) | [dolphin_windows_amd64.zip](https://github.com/juancavallotti/octo/releases/download/v0.5.0/dolphin_windows_amd64.zip) |
| Windows (arm64) | [dolphin_windows_arm64.zip](https://github.com/juancavallotti/octo/releases/download/v0.5.0/dolphin_windows_arm64.zip) |
| Both | [checksums.txt](https://github.com/juancavallotti/octo/releases/download/v0.5.0/checksums.txt) |

<!-- x-release-please-end-version -->

Have a Go toolchain? Install either CLI straight from source:

```bash
go install github.com/juancavallotti/octo/runtime/octo@latest
go install github.com/juancavallotti/octo/runtime/dolphin@latest
```

Prefer Docker? Two public images on Docker Hub, no install at all:

```bash
# The visual editor + runtime ("try Octo"): flows are read/written in $PWD.
docker run -p 3000:3000 -v "$PWD:/work" juancavallotti/octo

# The runtime alone: runs every .yaml in the mounted config directory.
docker run -p 8080:8080 -v "$PWD:/etc/octo/integrations" juancavallotti/octo-runtime
```

Building from source is one command (`task runtime:build`); see the
[installation docs](https://juancavallotti.github.io/octo/getting-started/installation).

## Layout

- `runtime/`: the Go module's packages — the runtime engine (`core`, `types`, `connectors`, `services`) and the two CLIs: `octo` (`runtime/octo`) and `dolphin` (`runtime/dolphin`), the test runner that drives it. The module itself is rooted at the repo root so both are `go install`-able.
- `apps/standalone/`: the single-user Next.js visual editor (Docker image `juancavallotti/octo`). See [apps/standalone/README.md](apps/standalone/README.md).
- `apps/platform/`: the orchestrator-backed multi-user Octo web app. See [apps/platform/README.md](apps/platform/README.md).
- `apps/docs/`: the documentation site (Fumadocs). Content in `apps/docs/content/docs/`.
- `packages/`: shared pnpm workspace libraries (`@octo/editor`, `@octo/mcp`, `@octo/run-host`, ...).
- `orchestrator/`: Go API that deploys integrations as Kubernetes workloads.
- `helm/`: Helm chart for the GCP deployment; `deploy/`: k8s manifests (local k3d) and Terraform (GCP).
- `docs/`: contributor policies (coding standards, lint, review, release) and internal deep-dives (deployment, processing pipeline).
- `samples/`: runnable flow examples used throughout the docs.

## Deployment

To run Octo on GCP (single-node k3s, Traefik, free Let's Encrypt TLS, and
per-integration subdomains), see the published
[deployment docs](https://juancavallotti.github.io/octo/deploy/) or the internal
[deployment guide](docs/deployment.md). For local development on k3d, use the
`task cluster:*` targets.

## Working rules

Read [AGENTS.md](AGENTS.md) before changing code.
Read [docs/coding-standards.md](docs/coding-standards.md) for code style and design rules.
Read [docs/linting-policy.md](docs/linting-policy.md) for lint expectations.
Read [docs/release-process.md](docs/release-process.md) before release-related work.

The Go runtime lives under [runtime/](runtime/); its module manifest is the root [go.mod](go.mod).

## Runtime architecture

Read [docs/processing-pipeline.md](docs/processing-pipeline.md) for the runtime
building blocks: connectors, message sources, flows, blocks/processors, composite
blocks (`handle-errors`/`fork`), the worker-pool concurrency model, the flow-event bus,
and the start/stop lifecycle. A minimal flow looks like:

```yaml
flows:
  - name: ingest-orders
    workers: 8
    source:
      connector: api
      type: http
      settings: { path: /orders }
    process:
      - type: validate
        rules:
          - { expr: "has(body.id)", message: "order id is required" }
          - { expr: "body.amount > 0", message: "amount must be positive" }
```

## Tasks

- `task fmt`
- `task test`
- `task build`
- `task tidy`
- `task lint-strict`
- `task policy-check`
- `task release-check`

Docs site:

- `task docs:dev`
- `task docs:build`
- `task docs:check`

Standalone editor (Next.js):

- `task dev`
