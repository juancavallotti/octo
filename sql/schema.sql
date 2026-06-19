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

-- integration_idx_structure is a folder tree organizing integrations. `parent_id` is
-- self-referencing and NULL for root folders.
CREATE TABLE IF NOT EXISTS integration_idx_structure (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_id uuid REFERENCES integration_idx_structure (id) ON DELETE CASCADE,
    name      varchar NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_integration_idx_structure_parent
    ON integration_idx_structure (parent_id);

-- integration_folder_members maps which folder holds which integrations. The composite PK
-- prevents duplicate membership rows.
CREATE TABLE IF NOT EXISTS integration_folder_members (
    folder_id      uuid NOT NULL REFERENCES integration_idx_structure (id) ON DELETE CASCADE,
    integration_id uuid NOT NULL REFERENCES integrations (id) ON DELETE CASCADE,
    PRIMARY KEY (folder_id, integration_id)
);

CREATE INDEX IF NOT EXISTS idx_integration_folder_members_integration
    ON integration_folder_members (integration_id);
