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
-- structured slog fields as JSON. The leading (deployment_id, ts DESC, id DESC)
-- index serves the common "tail one app's logs" query; the (ts DESC, id DESC) index
-- serves cross-deployment time scans and retention pruning.
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

-- The list view's orderings. Each carries id as a tiebreaker because the cursor is
-- composite: a burst of records routinely shares a timestamp, and a cursor on the
-- timestamp alone skips or repeats rows that tie across a page boundary.
CREATE INDEX IF NOT EXISTS idx_logs_deployment_ts_id ON logs (deployment_id, ts DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_logs_ts_id ON logs (ts DESC, id DESC);

-- The timestamp-only indexes these replace are now redundant prefixes of the pair
-- above: every query they served is served by the composite, and keeping them would
-- be write cost for nothing. Dropped rather than left, so an upgraded database ends
-- up with the same indexes a fresh one gets.
DROP INDEX IF EXISTS idx_logs_deployment_ts;
DROP INDEX IF EXISTS idx_logs_ts;

-- traces stores the trace records deployed runtimes ship over the internal.traces
-- NATS subject, persisted by the same aggregator service that owns `logs`. One row
-- is one record: the runtime's tracing has no span/parent-span model, so nesting is
-- reconstructed by a reader from `trace_id` (the group), `seq` (the order within one
-- publisher), `event_id` (the single message) and `path` (the block's address in the
-- grammar core/blockpath.go defines).
--
-- `deployment_id`, `app_name` and `app_version` arrive stamped by the emitting pod,
-- exactly as they do on a log record, so a trace keeps the exact integration version
-- that produced it even after the deployment is rolled to a new tag. `integration_id`
-- is resolved once at ingest from `deployment_id` (an immutable relation) so
-- per-integration cost queries never join back. Neither carries a foreign key, for
-- the same forensics reason `logs` gives.
--
-- `ts` is when the traced thing happened and `duration_ns` how long it took, so an
-- interval is [ts - duration_ns, ts] — the runtime stamps a post-invoke at completion.
-- A record that marks a point rather than an interval has duration_ns = 0.
--
-- `trace_id` defaults to '' rather than being nullable because the trace.dropped
-- marker — the in-band record announcing how many records the publisher had to throw
-- away — carries no trace id at all, and '' indexes without three-valued logic. It
-- carries no timestamp either, so ingest stamps one, the way a log record with no
-- `time` is stamped.
--
-- `body` and `vars` are the captured message payload, NULL when capture is off, when
-- the message had none, or when the payload was over the runtime's cap (`truncated`
-- says which). They are nullable rather than defaulting to '{}' because "not
-- captured" and "captured, and empty" are different facts.
CREATE TABLE IF NOT EXISTS traces (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    trace_id       varchar     NOT NULL DEFAULT '',
    seq            bigint      NOT NULL DEFAULT 0,
    kind           varchar     NOT NULL,
    event_id       varchar     NOT NULL DEFAULT '',
    correlation_id varchar     NOT NULL DEFAULT '',
    flow           varchar     NOT NULL DEFAULT '',
    path           varchar     NOT NULL DEFAULT '',
    block_type     varchar     NOT NULL DEFAULT '',
    ts             timestamptz NOT NULL,
    duration_ns    bigint      NOT NULL DEFAULT 0,
    error          text        NOT NULL DEFAULT '',
    dropped        boolean     NOT NULL DEFAULT false,
    truncated      boolean     NOT NULL DEFAULT false,
    body           jsonb,
    vars           jsonb,
    attrs          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    deployment_id  uuid        NOT NULL,
    integration_id uuid,
    app_name       varchar     NOT NULL DEFAULT '',
    app_version    varchar     NOT NULL DEFAULT '',
    received_at    timestamptz NOT NULL DEFAULT now(),

    -- Model-call accounting, lifted out of `attrs` onto columns so cost queries
    -- neither parse JSON nor re-derive arithmetic. Populated only for the llm.turn
    -- and llm.embed kinds; every other record leaves them at their defaults.
    --
    -- The token counts are NULLABLE on purpose. The runtime omits `attrs.usage`
    -- entirely when the provider reported nothing, and a provider reporting nothing
    -- is not a provider charging nothing — writing zeros here would erase that
    -- distinction and under-report every total built on it. Note also that
    -- output_tokens ALREADY INCLUDES thinking_tokens (the runtime normalizes every
    -- provider to that inclusive figure), so summing the two double-counts.
    --
    -- cost_usd is numeric, not an integer of micro-dollars: one token of a $0.075/1M
    -- model costs $7.5e-8, which micros round to zero. numeric also makes SUM() exact.
    --
    -- cost_status is what keeps "unpriced" from being read as "free". Every consumer
    -- has to look at it to know what a NULL cost means:
    --   ''             — not a model call
    --   priced         — fully priced from a known rate
    --   priced_partial — priced, but the rate card published no rate for one of the
    --                    cache halves, so those tokens were charged at the input
    --                    rate. The direction depends on which half: a cache READ
    --                    costs less than input, so the fallback over-states it; a
    --                    cache WRITE costs more, so the fallback under-states it.
    --   unpriced_model — the model is not in the rate card; cost is unknown, NOT zero
    --   no_usage       — the provider reported no usage; there is nothing to price
    --   reported       — the provider itself said what it charged. No rate produced
    --                    it and price_id is NULL: it is not an estimate but the
    --                    actual charge, including per-request and per-image
    --                    portions a token count cannot reconstruct. The most
    --                    certain of these statuses, not the least.
    -- price_id is the audit trail: which llm_prices row produced cost_usd. It is
    -- NULL for a reported cost, which is the one case where a known cost has no
    -- rate behind it.
    model           varchar NOT NULL DEFAULT '',
    provider        varchar NOT NULL DEFAULT '',
    input_tokens    integer,
    output_tokens   integer,
    thinking_tokens integer,
    cached_tokens   integer,
    cost_usd        numeric(20, 10),
    cost_status     varchar NOT NULL DEFAULT '',
    price_id        uuid
);

-- (trace_id, seq) serves the one query the detail view makes: every record of one
-- trace, in publication order. The two time indexes mirror the logs table's pair —
-- one app's records, and cross-deployment time scans (which retention pruning will
-- ride once it exists).
-- cache_write_tokens is the other half of prompt caching: cached_tokens counts
-- reads, this counts creation. They are kept apart because they bill in opposite
-- directions from ordinary input — a read cheaper, a write dearer (Anthropic
-- charges roughly 1.25x) — so a single "cache tokens" column could not price
-- either.
--
-- NULLABLE on the same rule as its neighbours, which is about the usage object
-- and not this field: NULL means the record carried no usage at all, and that
-- covers every row written before this column existed. A record that DID carry
-- usage stores 0 here when its provider reports no write count — OpenAI and
-- Gemini never do — exactly as thinking_tokens stores 0 for a provider that
-- reports no reasoning. Zero is "the provider accounted for this and it was
-- none"; NULL is "there was nothing to account for".
ALTER TABLE traces
    ADD COLUMN IF NOT EXISTS cache_write_tokens integer;

CREATE INDEX IF NOT EXISTS idx_traces_trace ON traces (trace_id, seq);
CREATE INDEX IF NOT EXISTS idx_traces_deployment_ts ON traces (deployment_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_traces_ts ON traces (ts DESC);

-- Cost rollups read only the two model-call kinds, which are a small fraction of the
-- records, so the index that serves them is partial rather than paying for every
-- block invocation.
CREATE INDEX IF NOT EXISTS idx_traces_llm ON traces (integration_id, ts DESC)
    WHERE kind IN ('llm.turn', 'llm.embed');

-- Dropped-record markers are rarer still, and are the only way a reader learns the
-- stream is incomplete. They carry no trace id, so they can only ever be reported per
-- app and per time window — which is exactly what this index serves.
CREATE INDEX IF NOT EXISTS idx_traces_dropped ON traces (deployment_id, ts DESC)
    WHERE kind = 'trace.dropped';

-- trace_summaries is one row per trace, maintained by upsert as records arrive.
--
-- It exists because the trace list is "one row per trace, newest first, keyset
-- paginated", and deriving that from `traces` with GROUP BY trace_id ORDER BY min(ts)
-- cannot use an index for the ordering: Postgres has to aggregate the whole qualifying
-- set before it can order it, so the hundredth page costs what the first one does and
-- both grow with total volume. A ten-block flow emits roughly two dozen records, so
-- this table is one to two orders of magnitude smaller than the one it summarizes.
--
-- EVERY column here must be an order-independent aggregate of the records that fed it.
-- Records arrive out of `seq` order (the publisher stamps the number and then queues
-- the record) and spread across ingest batches, so any rule that depends on which
-- record landed first is a rule that produces different answers on different runs.
-- The upsert accordingly uses LEAST/GREATEST, addition, de-duped array union, and a
-- ranked maximum for `status` (failed beats dropped beats ok) — never assignment.
--
-- `started_at` is MIN(ts - duration_ns), the same interval rule a waterfall applies,
-- so the duration this table reports and the span a chart draws are the same number by
-- construction rather than by two implementations agreeing.
--
-- A trace can cross deployments: the trace id rides on the message as an internal
-- variable and ships as a header over queues and topics, so a flow that dispatches to
-- another app keeps the same id. `deployment_id` is therefore the deployment of the
-- record that ENTERED the trace (whichever batch carried it fills the identity
-- columns), while `deployment_ids` accumulates every deployment seen.
--
-- `entry_kind`/`entry_label` name what started the trace: an HTTP request (method and
-- route pattern), a schedule (the cron expression), or failing both, the root flow.
-- The flow records carry no path and the source records carry no flow, which is why
-- this is captured at ingest rather than derived on read.
--
-- `cost_usd` sums only what could be priced; `unpriced_calls` counts what could not.
-- Keeping them apart is what stops an unknown model from silently reading as free.
CREATE TABLE IF NOT EXISTS trace_summaries (
    trace_id         varchar PRIMARY KEY,
    deployment_id    uuid        NOT NULL,
    deployment_ids   uuid[]      NOT NULL DEFAULT '{}',
    integration_id   uuid,
    app_name         varchar     NOT NULL DEFAULT '',
    app_version      varchar     NOT NULL DEFAULT '',
    started_at       timestamptz NOT NULL,
    ended_at         timestamptz NOT NULL,
    root_flow        varchar     NOT NULL DEFAULT '',
    entry_kind       varchar     NOT NULL DEFAULT '',
    entry_label      varchar     NOT NULL DEFAULT '',

    -- The three columns below are not reported to anyone: they are how the
    -- upsert decides which batch's version of a chosen column to keep.
    --
    -- Most of this table merges by bound, sum or set union, which need no
    -- history. The identity columns cannot: they are chosen from one particular
    -- record, and that record does not arrive first or even in the same batch as
    -- the records needing it — a flow.started is routinely written before the
    -- source.receive that supersedes it. Storing how good the winning record was
    -- lets a later batch correct an earlier guess while stopping a worse later
    -- record from overwriting a better earlier one, which neither
    -- first-writer-wins nor last-writer-wins can do in both directions.
    --
    -- entry_rank is lower-is-better and 99 means no entry record has been seen
    -- yet, so a stand-in is in place. root_flow_seq is tracked separately from
    -- entry_seq because the record naming the entry point is usually not the one
    -- naming the flow: source records carry no flow at all.
    entry_rank       smallint    NOT NULL DEFAULT 99,
    entry_seq        bigint      NOT NULL DEFAULT 0,
    root_flow_seq    bigint      NOT NULL DEFAULT 0,

    status           varchar     NOT NULL DEFAULT 'ok',
    root_duration_ns bigint      NOT NULL DEFAULT 0,
    records          integer     NOT NULL DEFAULT 0,
    llm_calls        integer     NOT NULL DEFAULT 0,
    input_tokens     bigint      NOT NULL DEFAULT 0,
    output_tokens    bigint      NOT NULL DEFAULT 0,
    cached_tokens    bigint      NOT NULL DEFAULT 0,
    cost_usd         numeric(20, 10) NOT NULL DEFAULT 0,
    unpriced_calls   integer     NOT NULL DEFAULT 0,
    models           text[]      NOT NULL DEFAULT '{}',
    first_seen_at    timestamptz NOT NULL DEFAULT now(),
    last_seen_at     timestamptz NOT NULL DEFAULT now()
);

-- The list view's orderings. Each carries trace_id as a tiebreaker because the cursor
-- is composite: traces routinely start within the same millisecond, and a cursor on
-- the timestamp alone skips or repeats rows that tie across a page boundary.
CREATE INDEX IF NOT EXISTS idx_trace_summaries_app_started
    ON trace_summaries (deployment_id, started_at DESC, trace_id DESC);
CREATE INDEX IF NOT EXISTS idx_trace_summaries_integration_started
    ON trace_summaries (integration_id, started_at DESC, trace_id DESC);
CREATE INDEX IF NOT EXISTS idx_trace_summaries_started
    ON trace_summaries (started_at DESC, trace_id DESC);

-- "Show me what broke" is the first thing anyone asks of a trace view, and failures
-- are a small minority of traces, so it gets a partial index of its own.
CREATE INDEX IF NOT EXISTS idx_trace_summaries_failed
    ON trace_summaries (deployment_id, started_at DESC)
    WHERE status <> 'ok';

-- llm_prices is the rate card the aggregator prices model calls from, kept as history
-- rather than as current state.
--
-- Cost is frozen onto the trace row at ingest, so a price change must never move a
-- number that has already been reported. Rows are therefore closed and superseded
-- (effective_to is stamped, a new row inserted) rather than updated in place, and the
-- row that priced a record is recorded on it as traces.price_id.
--
-- `model` is a PATTERN, not a served model id, and `operator` says how to apply it —
-- the vocabulary of the upstream catalogue: `equals` (exact), `startsWith` (prefix),
-- `includes` (substring). Resolution prefers the most specific match; see the cost
-- package for the full ordering. Rates are per one million tokens, as published.
--
-- The partial unique index is what makes "refresh only when a price actually changed"
-- enforceable rather than aspirational: at most one open row per pattern, so an
-- unchanged fetch is a no-op and a changed one is a close plus an insert. A pattern
-- that disappears from upstream is deliberately LEFT OPEN — a model vanishing from a
-- catalogue must not retroactively unprice the calls it already explained.
CREATE TABLE IF NOT EXISTS llm_prices (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source             varchar NOT NULL,
    provider           varchar NOT NULL,
    model              varchar NOT NULL,
    operator           varchar NOT NULL,
    input_per_1m       numeric(20, 10) NOT NULL,
    output_per_1m      numeric(20, 10) NOT NULL,
    cache_read_per_1m  numeric(20, 10),
    cache_write_per_1m numeric(20, 10),
    preferred          boolean     NOT NULL DEFAULT false,
    effective_from     timestamptz NOT NULL DEFAULT now(),
    effective_to       timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS llm_prices_current
    ON llm_prices (source, provider, model, operator)
    WHERE effective_to IS NULL;

CREATE INDEX IF NOT EXISTS idx_llm_prices_history
    ON llm_prices (source, provider, model, operator, effective_from DESC);

-- llm_price_syncs records every attempt to refresh the rate card, successful or not.
-- A pricing feed that quietly stopped answering looks exactly like a feed whose prices
-- stopped changing, and the difference is the one an operator needs: without this
-- table, costs drifting away from reality is invisible until someone checks a bill.
CREATE TABLE IF NOT EXISTS llm_price_syncs (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source     varchar     NOT NULL,
    fetched_at timestamptz NOT NULL DEFAULT now(),
    models     integer     NOT NULL DEFAULT 0,
    added      integer     NOT NULL DEFAULT 0,
    changed    integer     NOT NULL DEFAULT 0,
    ok         boolean     NOT NULL DEFAULT true,
    error      text        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_llm_price_syncs_source_time
    ON llm_price_syncs (source, fetched_at DESC);
