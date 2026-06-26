-- Octo database schema.
--
-- Applied by the schema startup Job (deploy/k8s/postgres-schema-job.yaml) against
-- the Postgres instance brought up by `task cluster:dev`. Written to be idempotent
-- (IF NOT EXISTS / ON CONFLICT) so the Job is safe to re-run on every deploy.

-- site_settings holds loosely-structured, per-site configuration as JSON, keyed by
-- a short string. The first key is db_version, used to track schema migrations.
CREATE TABLE IF NOT EXISTS site_settings (
    key   varchar PRIMARY KEY,
    value jsonb NOT NULL
);

-- Seed the schema version. `updated` is stamped with the apply-time date rather
-- than a frozen literal. ON CONFLICT keeps an existing value untouched, so a
-- later migration that bumps db_version is not clobbered by a re-run of this file.
INSERT INTO site_settings (key, value)
VALUES (
    'db_version',
    jsonb_build_object('version', 0, 'updated', CURRENT_DATE::text)
)
ON CONFLICT (key) DO NOTHING;

-- integrations holds the authored definition of each integration. `definition` is the raw
-- integration content (TEXT); `last_updated` is stamped by the application on write. Folders
-- and deployments reference this table.
CREATE TABLE IF NOT EXISTS integrations (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name         varchar NOT NULL,
    definition   text NOT NULL DEFAULT '',
    last_updated timestamptz NOT NULL DEFAULT now()
);

-- integration_deployments records each deployment of an integration. One integration may be
-- deployed many times; `settings` carries per-deployment config and `status` tracks lifecycle.
CREATE TABLE IF NOT EXISTS integration_deployments (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    integration_id uuid NOT NULL REFERENCES integrations (id) ON DELETE CASCADE,
    settings       jsonb NOT NULL DEFAULT '{}'::jsonb,
    status         varchar NOT NULL DEFAULT 'pending',
    last_updated   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_integration_deployments_integration
    ON integration_deployments (integration_id);

-- deployment_metadata holds orchestrator-owned bookkeeping about the live
-- Kubernetes resources (e.g. display name, last-observed pod conditions/URLs),
-- kept separate from `settings` which carries user-supplied per-deployment
-- config. Kubernetes resource identity is NOT stored: resources are named
-- deterministically from this row's id and resolved by the octo.dev/deployment-id
-- label. Added via ALTER so the idempotent schema upgrades existing tables.
ALTER TABLE integration_deployments
    ADD COLUMN IF NOT EXISTS deployment_metadata jsonb NOT NULL DEFAULT '{}'::jsonb;

-- cluster_secrets is the catalog of cluster-wide secret names. The VALUES live in a
-- single Kubernetes Secret (octo-secrets), never in the database; this table only
-- records each name and its timestamps so the UI can list secrets and show when
-- they were last set. `last_updated` is stamped by the application on every set.
CREATE TABLE IF NOT EXISTS cluster_secrets (
    name         varchar PRIMARY KEY,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_updated timestamptz NOT NULL DEFAULT now()
);

-- kv_store is the deployment-scoped key/value store the runtime services (the k8s
-- module) use for small state, including secrets. Keys are namespaced (e.g. system
-- vs user) so internal state stays isolated from user-configured blocks, and
-- `version` drives optimistic concurrency. Values in a secret namespace (a "_secrets"
-- suffix, e.g. system_secrets / user_secrets) are encrypted at rest by the
-- orchestrator with AES-GCM (KV_ENCRYPTION_KEY); plain namespaces are stored as-is,
-- so ordinary KV traffic pays no encryption cost. Rows are scoped by deployment_id
-- with no foreign key — cleanup is best-effort on undeploy — and the primary key's
-- leading deployment_id column lets a deployment's entries be dropped together.
CREATE TABLE IF NOT EXISTS kv_store (
    deployment_id uuid NOT NULL,
    namespace     varchar NOT NULL,
    key           varchar NOT NULL,
    value         bytea NOT NULL,
    version       bigint NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (deployment_id, namespace, key)
);

-- integration_idx_structure is a folder tree organizing integrations. `parent_id` is
-- self-referencing and NULL for root folders.
CREATE TABLE IF NOT EXISTS integration_idx_structure (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_id uuid REFERENCES integration_idx_structure (id) ON DELETE CASCADE,
    name      varchar NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_integration_idx_structure_parent
    ON integration_idx_structure (parent_id);

-- integration_folder_members maps which folder holds which integrations. An
-- integration lives in at most one folder, so integration_id is the primary key;
-- adding it to a folder moves it. The folder_id index serves "list a folder's
-- integrations".
CREATE TABLE IF NOT EXISTS integration_folder_members (
    integration_id uuid PRIMARY KEY REFERENCES integrations (id) ON DELETE CASCADE,
    folder_id      uuid NOT NULL REFERENCES integration_idx_structure (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_integration_folder_members_folder
    ON integration_folder_members (folder_id);
