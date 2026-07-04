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

-- Integration names are unique case-insensitively (global scope). The service
-- pre-checks for a clean error; this index is the backstop against races and
-- direct writes. Creation fails if pre-existing duplicates exist.
CREATE UNIQUE INDEX IF NOT EXISTS integrations_name_lower_uniq
    ON integrations (lower(name));

-- NOTE: integrations.created_by / updated_by are added after the users table is
-- defined (they reference it) — see "Integration attribution" near the end.

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

-- Manual ordering of folders among their siblings. New folders are appended
-- (assigned MAX(position)+1 within the parent); the tree lists by (position, name).
ALTER TABLE integration_idx_structure
    ADD COLUMN IF NOT EXISTS position int NOT NULL DEFAULT 0;

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

-- Manual ordering of integrations within a folder. New members are appended
-- (assigned MAX(position)+1); the middle column orders by (position, name).
ALTER TABLE integration_folder_members
    ADD COLUMN IF NOT EXISTS position int NOT NULL DEFAULT 0;

-- integration_snapshots freezes an integration's definition under a named tag.
-- Tags are immutable (no update path) and unique per integration; a deploy
-- references a snapshot so it ships a frozen definition rather than the live one.
CREATE TABLE IF NOT EXISTS integration_snapshots (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    integration_id uuid NOT NULL REFERENCES integrations (id) ON DELETE CASCADE,
    tag            varchar NOT NULL,
    definition     text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (integration_id, tag)
);

CREATE INDEX IF NOT EXISTS idx_integration_snapshots_integration
    ON integration_snapshots (integration_id);

-- integration_resources holds the live, mutable env/template resources authored for an
-- integration (the cloud counterpart to the files the standalone runtime reads from the
-- config directory). `kind` mirrors the runtime's core.ResourceKind ('env' | 'template')
-- and `name` is the resource id the config references — a path that may contain '/' (a
-- future feature uploads zip bundles that keep their relative paths), so a name is never
-- placed in a URL path segment. `content` is the raw text. UNIQUE (integration_id, name)
-- makes a path a single resource per integration.
CREATE TABLE IF NOT EXISTS integration_resources (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    integration_id uuid NOT NULL REFERENCES integrations (id) ON DELETE CASCADE,
    kind           varchar NOT NULL,
    name           varchar NOT NULL,
    content        text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_updated   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (integration_id, name)
);

CREATE INDEX IF NOT EXISTS idx_integration_resources_integration
    ON integration_resources (integration_id);

-- integration_resource_snapshots freezes an integration's resources when it is tagged,
-- so a deploy ships the resources that matched its frozen definition rather than the
-- live ones. Rows are copied from integration_resources inside the same transaction that
-- creates the integration_snapshots row; the CASCADE off the snapshot drops the frozen
-- resources when the tag is deleted.
CREATE TABLE IF NOT EXISTS integration_resource_snapshots (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_id uuid NOT NULL REFERENCES integration_snapshots (id) ON DELETE CASCADE,
    kind        varchar NOT NULL,
    name        varchar NOT NULL,
    content     text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (snapshot_id, name)
);

CREATE INDEX IF NOT EXISTS idx_integration_resource_snapshots_snapshot
    ON integration_resource_snapshots (snapshot_id);

-- users records each authenticated principal. Identity comes from the OIDC
-- provider; on first sign-in the platform bootstraps a row keyed by the stable
-- `subject` (the OIDC `sub`) and keeps email/name in sync on subsequent logins.
-- The generated `id` is the durable handle other tables (api_keys) reference, so
-- it survives IdP email changes. The local-dev (no-SSO) session uses a sentinel
-- subject so `task dev` still resolves to a real user row.
CREATE TABLE IF NOT EXISTS users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subject       varchar UNIQUE NOT NULL,
    email         varchar NOT NULL,
    name          varchar NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz NOT NULL DEFAULT now()
);

-- api_keys are per-user bearer tokens used to authenticate machine clients (the
-- platform MCP endpoint). The plaintext token is shown to the user exactly once at
-- creation; only its SHA-256 hash is stored, so a database leak exposes no usable
-- keys. `prefix`/`last4` are non-secret fragments kept for display. Deletion is a
-- soft revoke (`revoked_at`) so an audit trail survives; expiry is enforced by the
-- application against `expires_at`.
CREATE TABLE IF NOT EXISTS api_keys (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         varchar NOT NULL,
    key_hash     varchar NOT NULL,
    prefix       varchar NOT NULL,
    last4        varchar NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at   timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys (key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys (user_id);

-- Integration attribution: created_by / updated_by record who authored and last
-- edited an integration. Defined here (not with the integrations table) because
-- they reference users, which is created above. Nullable — a row may be written
-- without a known actor (e.g. the MCP path, or local dev without SSO). ON DELETE
-- SET NULL so removing a user doesn't cascade-delete their integrations.
ALTER TABLE integrations
    ADD COLUMN IF NOT EXISTS created_by uuid REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE integrations
    ADD COLUMN IF NOT EXISTS updated_by uuid REFERENCES users (id) ON DELETE SET NULL;

-- Backfill attribution for pre-existing rows when exactly one user exists (the
-- common single-operator install): every integration is theirs. Idempotent — it
-- only touches still-null rows and is a no-op once a second user appears.
UPDATE integrations
   SET created_by = COALESCE(created_by, (SELECT id FROM users)),
       updated_by = COALESCE(updated_by, (SELECT id FROM users))
 WHERE (created_by IS NULL OR updated_by IS NULL)
   AND (SELECT count(*) FROM users) = 1;

-- logs stores log events shipped by deployed runtimes over the internal.logs NATS
-- subject and persisted by the log-aggregator service. `deployment_id` attributes
-- each event to the deployment that emitted it (no foreign key — logs are kept for
-- forensics and may outlive the deployment, like kv_store cleanup is best-effort).
-- `app_name` and `app_version` denormalize the deployment's display name and tag as
-- they were on the emitting pod (stamped by the runtime), so listing/filtering logs
-- never has to join back to the deployment tables and a log keeps the exact version
-- that produced it even across a rollout. `ts` is the record's own timestamp;
-- `received_at` is when the aggregator stored it. `attrs` holds the remaining
-- structured slog fields as JSON. The leading (deployment_id, ts DESC) index serves
-- the common "tail one app's logs" query; the (ts DESC) index serves cross-
-- deployment time scans and retention pruning.
CREATE TABLE IF NOT EXISTS logs (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id uuid        NOT NULL,
    app_name      varchar     NOT NULL DEFAULT '',
    app_version   varchar     NOT NULL DEFAULT '',
    ts            timestamptz NOT NULL,
    level         varchar     NOT NULL,
    message       text        NOT NULL,
    attrs         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    received_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_logs_deployment_ts ON logs (deployment_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_logs_ts ON logs (ts DESC);
