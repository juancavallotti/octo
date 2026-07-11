# Octo

**Octo** is a cloud-native integration runtime. The `octo` repository holds its
stacks, including a Go workspace for the runtime engine and CLI.

**Documentation:** the docs site — overview, getting started, connector and CEL
reference, guides, and the AI/MCP story — is built from [`apps/docs/`](apps/docs/)
(Fumadocs, statically exported) and published with GitHub Pages at
<https://juancavallotti.github.io/octo/>. Preview locally with `task docs:dev`.

## Layout

- `runtime/`: active Go workspace for the runtime engine and CLI.
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

The Go runtime workspace lives under [runtime/](runtime/).

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
