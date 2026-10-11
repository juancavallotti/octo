# Dependency Updates

Two pieces of automation keep the repository's dependencies current and flag the
ones with known vulnerabilities. Neither merges anything: every change arrives as
a pull request and goes through `validate.yml` like any other.

## Renovate

[Renovate](https://docs.renovatebot.com/) runs as the Mend Renovate GitHub App,
configured by [`renovate.json5`](../renovate.json5). It watches:

| Ecosystem | Where |
| --- | --- |
| Go modules and the `go` directive | `go.mod`, `orchestrator/`, `observability/`, `iam/`, `sidecars/` |
| npm (pnpm workspace) and `packageManager` | every `package.json`, `pnpm-lock.yaml` |
| Node version | `.nvmrc` files, `node:` base images |
| Container images | every `Dockerfile*`, third-party images in `helm/values.yaml`, Cloud Build steps |
| GitHub Actions | `.github/workflows/` |
| Terraform providers and modules | `deploy/terraform/` (and its `.terraform.lock.hcl` files) |
| Pinned CLIs and cluster add-on charts | lines annotated with `# renovate:` (below) |

### How updates arrive

- **Routine updates** open early Monday (UTC), grouped so one PR moves a whole
  family — the Go toolchain everywhere it is pinned, Node everywhere, Kubernetes
  Go libraries, Terraform providers, non-major npm dev tooling, and so on.
- **Major updates** — and Go minor releases, which are toolchain changes — do not
  open on their own. They are listed on the *Dependency Dashboard* issue; tick one
  to have its PR raised.
- **Security updates** open immediately, ignore the schedule and the three-day
  release-age wait, are labelled `security`, and are committed as `fix(deps):` so
  release-please lists them and cuts a patch release.
- Commits are Conventional: `fix(deps):` for runtime dependencies, `chore(deps):`
  for everything else.
- Postgres, pgvector and Redis majors are disabled outright. Each is a data or
  licence migration and is done by hand.

### Annotating a version Renovate cannot see

A version pinned in a shell script, an action's `with:` block or a Terraform
variable default is invisible to Renovate's built-in managers. Put an annotation
on the line directly above it:

```yaml
# renovate: datasource=github-releases depName=helm/helm
HELM_VERSION=v3.21.3
```

```hcl
# renovate: datasource=helm depName=cert-manager registryUrl=https://charts.jetstack.io
default = "v1.17.1"
```

The annotated line must be `key: value`, `key=value` or `key = "value"`. Add
`extractVersion=^v(?<version>.*)$` when the upstream tag carries a `v` the pinned
value does not. Annotations are honoured in `.github/workflows/`, `cloudbuild.yaml`
and `deploy/terraform/`; extend `customManagers` in `renovate.json5` to add a path.

## govulncheck

[`vulnerability-scan.yml`](../.github/workflows/vulnerability-scan.yml) runs
`govulncheck` over each Go module daily and on any PR that changes a `go.mod`. It
catches what Renovate cannot fix by bumping a dependency — a vulnerability in the
Go standard library or toolchain — and fails only when the vulnerable code is
reachable from octo's own. A failure on the schedule means: bump the
`toolchain` directive in each `go.mod` (the *Go toolchain* Renovate group), or
upgrade the module it names.
