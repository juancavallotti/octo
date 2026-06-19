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
