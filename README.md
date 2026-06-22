# Octo

**Octo** is a cloud-native integration runtime. The `octo` repository holds its
stacks, including a Go workspace for the runtime engine and CLI.

**Website:** the project landing page — overview, architecture diagrams, the
supported enterprise integration patterns, and runnable samples — is published
with GitHub Pages from [`docs/`](docs/index.html) at
<https://juancavallotti.github.io/octo/>.

## Layout

- `runtime/`: active Go workspace for the runtime engine and CLI.
- `editor/`: **Octo**, the Next.js visual editor for integrations (standalone; run via npm). See [editor/README.md](editor/README.md).
- `orchestrator/`: Go API that deploys integrations as Kubernetes workloads.
- `helm/`: Helm chart for the GCP deployment; `deploy/`: k8s manifests (local k3d) and Terraform (GCP).
- `docs/`: coding standards, lint policy, review policy, release process, and the [deployment guide](docs/deployment.md).

## Deployment

To run Octo on GCP (single-node k3s, Traefik, free Let's Encrypt TLS, and
per-integration subdomains), see the **[deployment guide](docs/deployment.md)**.
For local development on k3d, use the `task cluster:*` targets.

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
      connector: orders-kafka
      type: topic
      settings: { topic: orders }
    process:
      - { type: validate, settings: { schema: order.schema.json } }
```

## Tasks

- `task fmt`
- `task test`
- `task build`
- `task tidy`
- `task lint-strict`
- `task policy-check`
- `task release-check`

Octo editor (Next.js):

- `task editor:install`
- `task editor:dev`
- `task editor:lint`
- `task editor:test`
- `task editor:build`
