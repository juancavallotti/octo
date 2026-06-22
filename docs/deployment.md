# Deploying Octo to GCP

This guide covers running Octo on Google Cloud: a single VM with single-node
**k3s**, **Traefik** ingress, and **cert-manager** for free auto-renewing TLS.
Octo's editor is served at your domain, and each deployed integration can get its
own subdomain.

For the bare command sequence see [deploy/terraform/README.md](../deploy/terraform/README.md);
this document is the full reference (architecture, configuration, operations,
troubleshooting). For local development on k3d instead, see
[the cluster tasks](#local-development-k3d).

---

## Architecture

```
                      Cloud DNS
   octo.juancavallotti.com  ─┐
   *.octo.juancavallotti.com ─┤ A records → static IP
                              │
                       ┌──────▼─────────────────────────────────────────┐
                       │ GCE VM (Debian 12, e2-standard-2) — single-node │
                       │ k3s                                             │
                       │                                                 │
                       │  Traefik :80/:443  ── cert-manager (Let's       │
                       │    │                   Encrypt, HTTP-01)        │
                       │    ├─ octo.…           → editor  :3000          │
                       │    └─ {slug}.octo.…    → integration pod :8080  │
                       │                                                 │
                       │  editor ─(BFF)→ orchestrator :8090 ─┐           │
                       │                                     │ client-go │
                       │  Postgres :5432 (StatefulSet)       ▼           │
                       │  per integration: ConfigMap + Deployment +      │
                       │    Service (octo-dep-{id}) [+ octo-int-{slug}]  │
                       │    [+ Ingress when exposed externally]          │
                       └─────────────────────────────────────────────────┘
                              ▲ images + OCI chart (pull)
                       Artifact Registry  ◄── Cloud Build (on version tag)
```

**Components** (all in namespace `octo-dev`):

| Component | Image | Port | Exposure |
|---|---|---|---|
| Platform (Next.js + bundled runtime) | `octo-platform` | 3000 | Ingress → your domain |
| Orchestrator (deploys integrations via the k8s API) | `octo-orchestrator` | 8090 | ClusterIP (internal; the platform proxies it) |
| Postgres | stock `postgres:16-alpine` | 5432 | ClusterIP (headless) |
| Schema applier (Helm hook job) | `octo-schema` | – | runs once per install/upgrade |
| Integration runtime (one pod set per deployment) | `octo-runtime` | 8080 | ClusterIP, optional Ingress |

**Who owns what:**

- **The VM** only *bootstraps* the cluster (k3s, cert-manager, the
  `letsencrypt-prod` ClusterIssuer, and an `octo-pull` helper). It does not hold
  any app source.
- **Terraform owns the Octo release** through the Helm provider: you `apply` the
  `release` root to install or upgrade the chart.
- **The orchestrator** creates the per-integration Kubernetes resources at runtime
  when you deploy an integration from the editor.

---

## Prerequisites

- `gcloud` authenticated: `gcloud auth application-default login`.
- A **Cloud DNS managed zone** for your domain already exists (you supply its
  name, e.g. `juancavallotti-com`).
- Tools on your workstation: `terraform` (>= 1.5), `helm`, `docker`, `gcloud`,
  and [`task`](https://taskfile.dev).
- Default region is **us-west1** (Oregon); override per root if needed.

---

## Terraform roots

Under [deploy/terraform/](../deploy/terraform/):

| Root | What it creates | When to apply |
|---|---|---|
| `infra/` | Artifact Registry repo (`octo`); VM, static IP, firewall, DNS records, k3s bootstrap; and (optional) the Cloud Build trigger + IAM | once per cluster |
| `release/` | The Octo Helm release (image tag + chart version) | every deploy/upgrade |

The three one-time pieces are composed in a single `infra/` root. **There is one
tfvars file** — `octo.tfvars` — read by both roots (per-deploy `image_tag`/
`chart_version` come from the command line). `release/` stays separate because it runs
on every deploy — on a version tag Cloud Build applies it for you, or run `task deploy`
by hand.

`release/` state lives in a GCS bucket (created once with `task state:bucket`, so Cloud
Build and your laptop share it); `infra/` keeps local state. The `release/` state holds
the generated secrets (the Postgres password, and — with SSO — the OIDC client secret
and Auth.js session secret), and the fetched kubeconfig holds cluster-admin
credentials — both are gitignored. Keep your state bucket locked down.

---

## One-time setup

```sh
gcloud auth application-default login

# 1. Fill in the one tfvars file (project_id, domain, dns_managed_zone)
cd deploy/terraform
cp octo.tfvars.example octo.tfvars

# 2. Remote state bucket for the release root (run once)
task state:bucket PROJECT=<your-project>

# 3. Everything one-time: registry + VM + k3s bootstrap (+ optional Cloud Build).
#    Leave enable_cloudbuild unset for the first apply.
task infra:apply
terraform -chdir=infra output                      # image_base, static_ip, url, kube_api_endpoint

# 4. (Optional) Cloud Build automation. Connect the GitHub repo once in the console
#    (Cloud Build → Triggers → Connect repository — installs the GitHub App), then
#    set enable_cloudbuild=true in octo.tfvars and re-run:
task infra:apply
```

> **Restrict access in production.** SSH (22) and the k3s API (6443) default to
> open. Set `ssh_source_ranges` and `kube_api_source_ranges` in `octo.tfvars`
> to your IP — but include the IAP range `35.235.240.0/20` so the Cloud Build deploy
> step (dynamic egress IP) can still SSH and reach 6443. The release apply needs 6443
> reachable from where Terraform runs.

---

## Publishing images and the chart

The node and the Helm provider pull from Artifact Registry, so images and the
chart must be published before (or as part of) a deploy.

**Automated (recommended):** push a version tag — release-please publishes
`vX.Y.Z`. The Cloud Build trigger runs [cloudbuild.yaml](../cloudbuild.yaml),
building all four images and the chart and pushing them to Artifact Registry,
tagged with both the git tag and `latest`.

**Manual:** with `IMAGE_BASE` from `terraform output image_base`:

```sh
gcloud auth configure-docker us-west1-docker.pkg.dev
helm registry login us-west1-docker.pkg.dev -u oauth2accesstoken -p "$(gcloud auth print-access-token)"
task images:push IMAGE_BASE=$IMAGE_BASE TAG=v0.1.1
task helm:push   IMAGE_BASE=$IMAGE_BASE
```

The chart version comes from [helm/Chart.yaml](../helm/Chart.yaml) (kept in step
with the repo release by release-please); image tags are whatever you push.

---

## Deploying and rolling upgrades

On a version tag, Cloud Build runs the release automatically (the `deploy` step in
[cloudbuild.yaml](../cloudbuild.yaml), gated on `_DEPLOY`). To deploy a published tag
by hand — fetches the kubeconfig and derives `chart_version` from `helm/Chart.yaml`:

```sh
task deploy TAG=v0.1.1            # optional: DOMAIN=… INSTANCE=… ZONE=…
```

Or apply the `release` root directly (state is in GCS, so `init` picks up the shared
state):

```sh
task deploy:kubeconfig DOMAIN=octo.juancavallotti.com
cd deploy/terraform/release
terraform init
terraform apply -var-file=../octo.tfvars -var image_tag=v0.1.1 -var chart_version=0.1.2
```

Each apply:

1. Runs `octo-pull` on the node over SSH (a fresh registry token) to pull the
   target image tag into containerd — so the chart's pods (`imagePullPolicy:
   IfNotPresent`), including the per-integration runtime image, find it locally.
2. Installs/upgrades the chart through the Helm provider, passing the Postgres
   password it holds in state and authenticating the OCI chart pull with your
   GCP token.

A changed `image_tag` rewrites the pod templates, so the editor/orchestrator
Deployments roll automatically; Postgres is untouched when only the tag moves.
The first TLS issuance takes a minute after the DNS A record resolves.

> The release apply shells out to `gcloud compute ssh` (for `octo-pull`) and to
> the k3s API (helm/kubernetes providers) — so it needs `gcloud`, the kubeconfig,
> and 6443 reachable from where you run it.

---

## Integration endpoints

When you deploy an integration from the editor, the orchestrator creates a
ConfigMap + Deployment + Service named `octo-dep-{deploymentId}` in `octo-dev`.
Two endpoint modes are available per deployment:

### Internal (default)

- The per-deployment `Service` load-balances across the deployment's **replicas**
  (set the replica count in the editor's deploy form).
- A stable, integration-scoped Service `octo-int-{slug}` (slug of the integration
  name) lets other flows reach the integration at a constant address regardless of
  deployment id, balanced across all its replicas:

  ```
  http://octo-int-{slug}.octo-dev:8080
  ```

  This URL is reported back in the deployment's `internalUrl`. The shared Service
  is removed when the last deployment of that integration is undeployed.

### External

Toggle **Expose externally** (and optionally type a subdomain; it defaults to the
integration slug). The orchestrator additionally creates a Traefik `Ingress`:

```
https://{subdomain}.{baseDomain}     e.g. https://orders.octo.juancavallotti.com
```

- TLS is issued per host by cert-manager via HTTP-01 (the `*.{domain}` wildcard
  record resolves every subdomain to the VM).
- The public URL is reported as the deployment's `externalUrl`.
- External endpoints require `BASE_DOMAIN` on the orchestrator (the `release` root
  sets it to your domain); without it, an external request returns 400.

Use distinct subdomains across externally-exposed deployments — two Ingresses on
the same host route ambiguously.

---

## Configuration reference

All settings live in the single `octo.tfvars`, read by both roots. Per-deploy values
(`image_tag`, `chart_version`) come from the command line instead.

### `infra` variables (set in `octo.tfvars`)

| Variable | Default | Notes |
|---|---|---|
| `project_id` | – | required |
| `domain` | `octo.juancavallotti.com` | editor host; subdomains under `*.{domain}` |
| `dns_managed_zone` | – | required; Cloud DNS zone name |
| `machine_type` | `e2-standard-2` | 8 GB; `e2-medium` (4 GB) is tight |
| `ssh_source_ranges` | `["0.0.0.0/0"]` | restrict in production (keep the IAP range) |
| `kube_api_source_ranges` | `null` (= SSH ranges) | who can reach 6443 |
| `acme_email` | `juancavallotti@gmail.com` | Let's Encrypt account |
| `enable_cloudbuild` | `false` | create the trigger (needs GitHub App connected first) |
| `cloudbuild_auto_deploy` | `true` | also roll the cluster on a tag (`_DEPLOY=true` + deploy IAM) |
| `oidc_enabled` | `false` | gate the editor behind eetr SSO (consumed by the `release` root) |
| `oidc_client_id` | `""` | IdP client id (non-secret) |
| `oidc_client_secret` | `""` | IdP client secret (kept in release state, not Secret Manager) |
| `oidc_issuer` | `https://auth.eetr.app` | OIDC issuer |
| `oidc_write_roles` | `""` | roles allowed to write; empty = any signed-in user |
| `oidc_roles_claim` | `""` | id-token claim for roles (Auth.js default `roles`) |

### `release` root variables

| Variable | Default | Notes |
|---|---|---|
| `image_tag` | `latest` | bump to roll an upgrade |
| `chart_version` | – (required) | must match the published `helm/Chart.yaml`; derived by Cloud Build / `task deploy` |
| `cluster_issuer` | `letsencrypt-prod` | cert-manager issuer |
| `kubeconfig` | `../infra/kubeconfig.yaml` | from `task deploy:kubeconfig` |

### Editor SSO (OIDC, optional)

Set `oidc_enabled = true` plus `oidc_client_id` / `oidc_client_secret` in `octo.tfvars`.
The `release` root consumes these directly (this setup uses no Secret Manager — all
generated credentials live in the bucket-backed release state) and generates the
Auth.js session secret. The
chart creates the `{release}-auth` Secret for the client secret + session secret, and
the editor gets `AUTH_EETR_ISSUER`, `AUTH_EETR_CLIENT_ID` (plain), `AUTH_EETR_CLIENT_SECRET`,
`AUTH_SECRET` (from the Secret), plus `AUTH_URL` and `AUTH_TRUST_HOST`. Auth turns on
automatically once those are present; with `oidc_enabled = false` the editor stays open.

The client secret and session secret live in the release Terraform state (the GCS
bucket), consistent with the Postgres password — keep the state bucket locked down.
Register the redirect URI on the IdP: `https://{domain}/api/auth/callback/eetr`. The
orchestrator stays in-cluster only and is reached solely through the editor's
authenticated BFF (no separate token).

### Orchestrator environment (set by the chart)

| Var | Purpose |
|---|---|
| `DATABASE_URL` | Postgres DSN |
| `KUBE_NAMESPACE` | namespace for integration workloads (`octo-dev`) |
| `RUNTIME_IMAGE` | image deployed per integration (`{image_base}/octo-runtime:{tag}`) |
| `BASE_DOMAIN` | parent domain for external endpoints; empty disables them |
| `CLUSTER_ISSUER` | cert-manager issuer for external TLS (`letsencrypt-prod`) |

Chart values are documented in [helm/values.yaml](../helm/values.yaml).

---

## Operations

```sh
# SSH to the VM
gcloud compute ssh octo --zone us-west1-a

# Cluster state
gcloud compute ssh octo --zone us-west1-a -- sudo k3s kubectl get pods -n octo-dev
gcloud compute ssh octo --zone us-west1-a -- sudo k3s kubectl get ingress,certificate -n octo-dev

# Platform / orchestrator logs
sudo k3s kubectl logs -n octo-dev deploy/octo-platform
sudo k3s kubectl logs -n octo-dev deploy/octo-orchestrator
```

- **Re-bootstrap the VM:** the startup script is guarded by `/opt/octo/.provisioned`.
  To re-run it: `sudo rm /opt/octo/.provisioned && sudo reboot`.
- **Postgres data** lives on the boot disk (k3s `local-path`); it survives reboots
  but is destroyed if the VM is destroyed. The password is generated and held in the
  `release` Terraform state (the GCS bucket) — `terraform output` it if you need it.
- **Re-pull a tag manually on the node:** `sudo octo-pull v0.1.2`.

---

## Troubleshooting

- **TLS stuck / browser shows a default cert.** Confirm the A record resolves to
  the static IP and port 80 is reachable, then check the Certificate:
  `kubectl describe certificate -n octo-dev`. cert-manager needs the HTTP-01
  challenge to reach Traefik.
- **Pods `ImagePullBackOff`.** The node pulls with a token that expires ~1h after
  boot; a *new* tag deployed long after provisioning needs a fresh pull. The
  `release` apply runs `octo-pull` for you; to do it by hand, SSH in and
  `sudo octo-pull <tag>`.
- **`terraform apply` (release) can't reach the cluster.** Ensure 6443 is open to
  your IP (`kube_api_source_ranges`) and the kubeconfig is current
  (`task deploy:kubeconfig DOMAIN=...`).
- **External deploy returns 400.** `BASE_DOMAIN` isn't set on the orchestrator —
  redeploy the `release` root (it sets it from `domain`).
- **Cloud Build trigger doesn't fire.** The GitHub repo must be connected once in
  the console (the GitHub App install can't be done by Terraform).

---

## Local development (k3d)

For day-to-day development the same images run on a local k3d cluster via DevSpace
— no GCP, no Terraform. See the `cluster:*` tasks
([Taskfile.yml](../Taskfile.yml)): `task cluster:deploy` brings up k3d + Postgres +
the apps from [deploy/k8s/](../deploy/k8s/), and `task cluster:dev` adds hot reload.
That path uses raw manifests and `k3d image import`; the Helm chart and Terraform
here are for the GCP deployment.
