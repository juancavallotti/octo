# Deploying octo with Terraform

Five roots. One is the production single-VM k3s deployment; three stand up managed
clusters to exercise the chart's cloud profiles; one owns the Helm release on the
k3s VM.

> This README is the command quick-reference. For the full guide to the k3s
> deployment — architecture, configuration reference, integration endpoints,
> operations and troubleshooting — see [docs/deployment.md](../../docs/deployment.md).

## Layout

### Roots (things you apply)

| Root | What it is | State prefix | Apply |
|---|---|---|---|
| `infra/` | The reference deployment's one-time infrastructure: Artifact Registry, the single-node k3s VM with its Postgres data disk, and optionally the Cloud Build trigger. | `infra` | once |
| `release/` | The octo Helm release on that VM. Terraform owns it — do not `helm upgrade` it by hand. | `release` | per deploy |
| `gke-standard/` | A GKE Standard cluster + prerequisites + the chart, using `helm/values-gke-standard.yaml`. | `gke-standard` | test |
| `gke-autopilot/` | The same, on GKE Autopilot, using `helm/values-gke-autopilot.yaml`. | `gke-autopilot` | test |
| `eks/` | An EKS cluster + VPC + ALB controller + the chart, using `helm/values-eks.yaml`. | `eks/…` (S3) | test |

The three cluster roots exist to run the chart's cloud profiles on real clusters.
They are meant to be brought up, tested and destroyed. **Run one at a time** — the
two GKE roots write the same DNS records by default.

They are a worked example, not production infrastructure, and the difference is
worth being explicit about because the code does not look like a toy:

- **Each root deploys the application as well as the cluster.** `module.octo` is a
  `helm_release` inside the root, so `terraform apply` is also a redeploy. Fine when
  the point is to test a chart change end to end; wrong when infrastructure and
  application releases should move on separate schedules.
- **There is one environment.** No staging/production split, no per-environment
  state or hostnames — the roots deliberately share names so only one can exist at
  a time.
- **The defaults are cheap and disposable**, not safe: spot nodes, public nodes on
  EKS, no deletion protection, no database backups. Each has a variable that turns
  it into the production choice, but nothing checks that you set them.

The stable interface is the **chart**, not this Terraform. Everything these roots
configure — hostnames, TLS mode, external database, secrets — is chart values, and
`modules/helm-release` is a thin wrapper around `helm_release` that any of your own
modules could replace. Read these for the wiring, then bring your own infrastructure.

### Modules (things roots call)

Each has its own `README.md`.

| Module | Used by |
|---|---|
| `modules/registry` · `modules/base` · `modules/cloudbuild` | `infra` |
| `modules/helm-release` | every root — the octo release itself, cloud-agnostic |
| `modules/gke-cluster` · `modules/cluster-addons` · `modules/cloudsql` | via `modules/octo-gke` |
| `modules/octo-gke` | both GKE roots — the shared composition, so they cannot drift |
| `modules/eks-addons` · `modules/rds` | `eks` |
| `charts/cluster-issuers` | a local Helm chart, not a Terraform module — see [`charts/README.md`](charts/README.md) |

## Conventions

Each root is **self-contained**:

- **Variables** live in that root's own `terraform.tfvars` (gitignored; copy the
  committed `terraform.tfvars.example`). Terraform loads that filename
  automatically, so no `-var-file` is passed anywhere. Values like `project_id` and
  `domain` therefore repeat across roots — deliberately: one file tells you
  everything a root needs, and every root declares exactly what it uses, so a stale
  or misspelled name is a hard error rather than a warning nobody reads.
- **State** is remote and versioned, one prefix per root. Never local: losing it
  orphans clusters and disks with nothing left able to destroy them. The bucket
  comes from `backend.hcl` (GCS) or `backend-aws.hcl` (S3) at init time, because a
  backend block cannot reference variables. Copy the `.example` files.
- **Provider versions** are pinned by a committed `.terraform.lock.hcl` per root,
  with `darwin_arm64` and `linux_amd64` hashes, so a laptop, Cloud Build and CI all
  run identical providers. Regenerate after changing a constraint:
  ```sh
  terraform -chdir=<root> providers lock -platform=darwin_arm64 -platform=linux_amd64
  ```
- **`task tf:check`** runs `terraform fmt -check` plus `validate` on every root. It
  needs no credentials and runs in CI.

Region defaults to **us-west1** on GCP, **us-east-1** on AWS.

## One-time setup

```sh
gcloud auth application-default login

# 1. State bucket, shared by every GCP root.
task state:bucket PROJECT=<your-project>
cp backend.hcl.example backend.hcl        # set bucket = octo-tfstate-<your-project>

# 2. Variables for the root you are applying.
cp infra/terraform.tfvars.example infra/terraform.tfvars
cp release/terraform.tfvars.example release/terraform.tfvars

# 3. The reference deployment: registry + VM + k3s bootstrap. Leave
#    enable_cloudbuild unset for now — the trigger needs the GitHub App connected.
task infra:apply
terraform -chdir=infra output      # static_ip, url, kube_api_endpoint, postgres_host_path

# 4. (Optional) Cloud Build. Connect the GitHub repo once in the console
#    (Cloud Build → Triggers → Connect repository), set enable_cloudbuild = true
#    in infra/terraform.tfvars, and re-run:
task infra:apply
```

### DNS zones for the cluster roots

The cluster roots create DNS **records** but never the **zone**. That is deliberate:
Cloud DNS and Route53 both assign fresh nameservers on every zone creation, so a
zone owned by a root you destroy and recreate would invalidate its delegation every
cycle. Create each zone once, by hand, and delegate it from whoever holds the parent
domain:

```sh
# GCP — for both GKE roots
gcloud dns managed-zones create gke-octopaas-dev \
  --dns-name=gke.octopaas.dev. --description="octo GKE test clusters"
gcloud dns managed-zones describe gke-octopaas-dev --format='value(nameServers)'

# AWS — for the EKS root
aws route53 create-hosted-zone --name eks.octopaas.dev --caller-reference "octo-$(date +%s)" \
  --query 'DelegationSet.NameServers'
```

Then add an `NS` record for each subdomain at the parent domain's DNS provider,
pointing at those nameservers.

The zone's **name** has to be the name you delegate. Route53's nameservers are
shared across every customer and answer only for zones actually hosted on them,
matched by name — so pointing an `eks.octopaas.dev` delegation at the nameservers
of a zone called `aws.octopaas.dev` gets `REFUSED`, even though those really are
Route53 nameservers and the record looks correct. Verify by asking a delegated
nameserver directly rather than trusting the values:

```sh
dig NS gke.octopaas.dev @ns-cloud-e1.googledomains.com   # expect NOERROR
dig NS eks.octopaas.dev @ns-336.awsdns-42.com            # expect NOERROR, not REFUSED
```

`dig NS <zone> +short` on its own is not a check: it returns empty both when the
delegation is missing and when it points somewhere that refuses.

## Publish images + charts

- **Automated:** push a version tag (release-please publishes `vX.Y.Z`) — the Cloud
  Build trigger builds all five images and both charts, pushes them to Artifact
  Registry, renders a digest-pinned `dist/values.images.yaml`, and (with
  `_DEPLOY=true`, the default when `cloudbuild_auto_deploy` is on) rolls the cluster.
- **Manual:** from the repo root, with `IMAGE_BASE` = `<region>-docker.pkg.dev/<project>/octo`:
  ```sh
  gcloud auth configure-docker us-west1-docker.pkg.dev
  helm registry login us-west1-docker.pkg.dev -u oauth2accesstoken -p "$(gcloud auth print-access-token)"
  task images:push IMAGE_BASE=$IMAGE_BASE TAG=v0.1.1
  task helm:push   IMAGE_BASE=$IMAGE_BASE
  ```

## Deploy / roll upgrades (k3s)

On a version tag Cloud Build does this automatically. By hand — fetches the
kubeconfig, derives the chart version from `helm/Chart.yaml`, applies the release
root:

```sh
task deploy TAG=v0.1.1            # optional: DOMAIN=… INSTANCE=… ZONE=…
```

Each apply pulls the target tag onto the node (fresh token, via `octo-pull` over
SSH), then installs/upgrades the chart. Bumping the tag rewrites the pod templates,
so the Deployments roll automatically; Postgres is untouched when only the tag
moves. First TLS issuance takes a minute after DNS resolves.

**Digest pinning.** Cloud Build passes `-var image_values_file=…` naming the exact
digest of every image it pushed, so the release runs what that build produced rather
than whatever the tag resolves to later; `image_tag` is then not passed to the chart
at all. A manual `task deploy` leaves it empty and goes by tag. Render one locally
with `task helm:values:images IMAGE_BASE=… TAG=…`.

**Database durability.** Postgres lives on a dedicated persistent disk, not the boot
disk: `modules/base` creates and attaches it, the startup script formats and mounts
it at `/mnt/octo-data`, and the release root pins the database to
`/mnt/octo-data/postgres` (`postgres_host_path`). The disk is a resource of its own
and is never auto-deleted with the instance, so the data survives a VM rebuild —
which neither the boot disk nor k3s's `local-path` provisioner (whose directory is
named after the claim's UID) would. Daily snapshots are kept for 7 days and outlive
the disk itself. Grow it by raising `data_disk_size_gb` and rebooting; `resize2fs`
runs on every boot.

Changing `postgres_host_path` on a release that already holds data is a data move,
not a config change: see [docs/deployment.md](../../docs/deployment.md).

**OIDC + Cloud Build:** `release/terraform.tfvars` is gitignored, so the Cloud Build
deploy step never sees it — it passes the non-secret config via `-var` from
substitutions and reads the OIDC creds back from `release/oidc.json` in the state
bucket. That file is written by a local `task deploy`, so **run `task deploy` once
after changing any `oidc_*` value** to reseed it before relying on the automated build.

Verify:

```sh
curl -I https://<domain>                                   # valid Let's Encrypt cert
gcloud compute ssh octo --zone us-west1-a -- sudo k3s kubectl get pods -n octo
```

## Test a managed cluster

Each root reads its **own** `terraform.tfvars` — nothing is shared between them, so
this step is per root and not optional:

```sh
cp gke-standard/terraform.tfvars.example gke-standard/terraform.tfvars
# gke-standard, gke-autopilot: project_id, acme_email, dns_managed_zone, domain,
#   apps_domain. The two roots default to the SAME hostnames — run one at a time.
# eks:          route53_zone_name, domain, apps_domain (no project_id; the AWS
#   account comes from your credentials).
```

Then:

```sh
# Build the working tree's images — the published ones track releases, so they do
# not match a chart you have edited since.
task images:ttl TTL=12h

task gke:standard:apply IMAGE_VALUES=dist/values.ttl.yaml
task gke:kubeconfig ROOT=gke-standard
kubectl -n octo get pods,pvc,ingress,certificate

# Flip external_database = true in the root's terraform.tfvars and re-apply to
# exercise Cloud SQL / RDS instead of the chart's bundled Postgres StatefulSet.

task gke:standard:destroy
```

Swap `gke:standard` for `gke:autopilot` to test the other flavour — same commands,
same tfvars keys, a separate state prefix. Set `IMAGE_VALUES` once per root, or put
`image_values_file` in the tfvars; note that `file()` resolves relative to the root
directory, so a repo-relative path there needs to be written `../../../dist/…`.

`eks:apply` additionally needs `task eks:state:bucket BUCKET=<name>` and
`backend-aws.hcl` once. **The EKS control plane is ~$0.10/hour with no free tier**,
billed from the moment the cluster exists — destroy the root when you are done. Keep
`kubernetes_version` on a version in *standard* support: extended support is
$0.60/hour for the identical cluster.

### Why destroy is phased

The ingress controller (GKE) and the AWS Load Balancer Controller (EKS) create cloud
load balancers that Terraform does not know about; they are cleaned up only when the
controller observes its Service or Ingress being deleted. Destroying everything at
once races that, and the orphaned forwarding rule or ALB then blocks the VPC delete
some twenty minutes later, with an error naming a leftover network interface rather
than the cause. The `*:destroy` tasks remove the release, then the controllers, then
the rest. Terraform's dependency graph gets this right within a single destroy; the
phases are what save you when one errors partway through.
