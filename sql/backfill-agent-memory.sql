-- Backfill: Dr. Octo's hand-kept conversation records into the agent memory tables.
--
-- Before the runtime had anywhere durable to put a transcript, Dr. Octo recorded
-- one himself into KV, from flows in his own definition:
--
--   agent-chat/{userKey}/{threadId}   the conversation, {"title", "turns":[{role,text}]}
--   agent-threads/{userKey}           an index, because core.KV has no list
--   facts/{userKey}                   {"items":[{name,value}]} — what he remembered
--
-- Those rows are in kv_store in this same database, so this is an INSERT … SELECT
-- with jsonb unpacking rather than a data migration in any interesting sense.
--
-- Run it ONCE per installation, after rolling out the Dr. Octo that no longer
-- writes them. It is idempotent — every insert is ON CONFLICT DO NOTHING — so
-- re-running it is safe, and running it once new conversations exist does not
-- disturb them.
--
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f sql/backfill-agent-memory.sql
--
-- What it does NOT do, deliberately:
--
--   * It does not delete the old KV rows. They are harmless where they are, and a
--     backfill that destroyed its own source could not be re-run after a mistake.
--     Sweep them by hand once you are satisfied.
--   * It does not fabricate a timestamp per turn. The record kept one time per
--     conversation, not per turn, so every turn of a migrated conversation shares
--     the conversation's own last-written time. A turn that claims to know when it
--     was said, and does not, is worse than one that is honestly dated together
--     with the rest.
--
-- The agent id is 'dr-octo', which is what his definition declares. An install
-- where someone has edited that has to edit it here too.

BEGIN;

-- Every deployment Dr. Octo has been installed as, and the integration behind it.
-- Every one, not just the current: a reinstall minted a new deployment id and
-- stranded the previous conversations under the old one, which is the bug this
-- whole feature exists to end — and those are exactly the conversations worth
-- rescuing.
CREATE TEMPORARY TABLE octo_backfill_scope ON COMMIT DROP AS
SELECT DISTINCT d.id AS deployment_id, d.integration_id
  FROM integration_deployments d
  JOIN kv_store k ON k.deployment_id = d.id
 WHERE k.namespace = 'user'
   AND (k.key LIKE 'agent-chat/%' OR k.key LIKE 'facts/%');

-- Conversations. The key is agent-chat/{userKey}/{threadId}, and a userKey can
-- itself contain a slash, so the thread id is the LAST segment and the user is
-- everything between the prefix and it.
CREATE TEMPORARY TABLE octo_backfill_chats ON COMMIT DROP AS
SELECT s.integration_id,
       substring(k.key from '^agent-chat/(.*)/[^/]*$') AS user_id,
       substring(k.key from '([^/]*)$')                AS thread_key,
       convert_from(k.value, 'UTF8')::jsonb            AS doc,
       k.updated_at
  FROM kv_store k
  JOIN octo_backfill_scope s ON s.deployment_id = k.deployment_id
 WHERE k.namespace = 'user'
   AND k.key LIKE 'agent-chat/%/%';

INSERT INTO agent_threads (
    integration_id, agent_id, thread_key, user_id, title, version, turn_count,
    created_at, last_activity_at)
SELECT c.integration_id,
       'dr-octo',
       c.thread_key,
       c.user_id,
       coalesce(c.doc ->> 'title', ''),
       1,
       coalesce(jsonb_array_length(c.doc -> 'turns'), 0),
       c.updated_at,
       c.updated_at
  FROM octo_backfill_chats c
 WHERE c.thread_key <> ''
ON CONFLICT (integration_id, agent_id, thread_key) DO NOTHING;

-- Turns, numbered by their position in the stored array — the order they were
-- appended in, and the only ordering the record ever had.
INSERT INTO agent_turns (thread_id, seq, role, text, attrs, created_at)
SELECT t.id,
       turn.ord,
       CASE WHEN turn.value ->> 'role' = 'user' THEN 'user' ELSE 'assistant' END,
       coalesce(turn.value ->> 'text', ''),
       jsonb_build_object('backfilled', true),
       c.updated_at
  FROM octo_backfill_chats c
  JOIN agent_threads t
    ON t.integration_id = c.integration_id
   AND t.agent_id = 'dr-octo'
   AND t.thread_key = c.thread_key
  CROSS JOIN LATERAL jsonb_array_elements(coalesce(c.doc -> 'turns', '[]'::jsonb))
       WITH ORDINALITY AS turn(value, ord)
 WHERE coalesce(turn.value ->> 'text', '') <> ''
ON CONFLICT (thread_id, seq) DO NOTHING;

-- What he was told to remember. One object per user held every fact, because KV
-- could not list; each becomes a row.
INSERT INTO agent_user_memories (
    integration_id, agent_id, user_id, name, value, version, created_at, updated_at)
SELECT s.integration_id,
       'dr-octo',
       substring(k.key from '^facts/(.*)$'),
       fact.value ->> 'name',
       fact.value ->> 'value',
       1,
       k.updated_at,
       k.updated_at
  FROM kv_store k
  JOIN octo_backfill_scope s ON s.deployment_id = k.deployment_id
  CROSS JOIN LATERAL jsonb_array_elements(
       coalesce(convert_from(k.value, 'UTF8')::jsonb -> 'items', '[]'::jsonb)) AS fact(value)
 WHERE k.namespace = 'user'
   AND k.key LIKE 'facts/%'
   AND coalesce(fact.value ->> 'name', '') <> ''
   AND coalesce(fact.value ->> 'value', '') <> ''
ON CONFLICT (integration_id, agent_id, user_id, name) DO NOTHING;

-- The thread index (agent-threads/{userKey}) is deliberately NOT migrated. It
-- existed only because KV could not enumerate; agent_threads is the listing now,
-- and a second copy of it is precisely what this feature removed.

COMMIT;

-- What landed, so the run reports something rather than finishing silently.
SELECT 'threads' AS what, count(*) FROM agent_threads WHERE agent_id = 'dr-octo'
UNION ALL
SELECT 'turns', count(*) FROM agent_turns tn
  JOIN agent_threads t ON t.id = tn.thread_id WHERE t.agent_id = 'dr-octo'
UNION ALL
SELECT 'memories', count(*) FROM agent_user_memories WHERE agent_id = 'dr-octo';
