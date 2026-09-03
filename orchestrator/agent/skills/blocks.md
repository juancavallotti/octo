# Blocks

The complete registry, as the running binary reports it. A block not listed here
does not exist — do not invent one, and do not assume a block from another tool has
an equivalent.

Remember the leaf/composite distinction: composites use top-level **slots**, leaves
put everything under `settings`.

## Control flow (composites)

| Block | Slots | What it does |
| --- | --- | --- |
| `if` | `condition`, `then`, `else` | Branch on a CEL boolean. |
| `switch` | `cases`, `default` | Ordered guarded branches; first matching `when` runs. |
| `foreach` | `items`, `as`, `process` | Iterate an array, running the body per element. |
| `fork` | `branches` | Run branches in parallel on copies, then join. |
| `split` | `items`, `process` | Turn one message into many; each continues down the rest of the flow. |
| `aggregate` | `key`, `process` | Combine many messages into one, grouped by an expression. |
| `enrich` | `body`, `setBody`, `setVars` | Run a body flow on an isolated copy, then merge results back. |
| `validate` | `rules`, `onReject`, `rejectStatus` | Assert CEL rules; a failure stops the flow. |
| `handle-errors` | `process`, `error` | Inline recovery; the failure is `vars.error`. |
| `cache-scope` | `key`, `ttl`, `body` | Memoize the body flow's result under a key. |
| `ai-agent` | see the AI section | Tool-calling loop. |
| `ai-router` | `connector`, `prompt`, `routes` | The model picks a named branch. |
| `ai-retry` | `connector`, `process`, `maxAttempts` | On error, the model revises and retries. |
| `mcp-router` | `serverName`, `tools`, `resources`, `prompts` | Serve a flow as an MCP server. |
| `breakpoint`, `spy`, `mock` | — | Editor/testing only. |

## Data

| Block | Key settings |
| --- | --- |
| `set-payload` | `value` — CEL; replaces the body. |
| `set-variable` | `name`, `value` — CEL; stores a variable. |
| `delete-variable` | `name`. |
| `multi-transform` | An ordered list of additive CEL edits in one block. |
| `template-resource` | `resource` (alias), and where to write the rendered output. |
| `log` | `logger`, `message` — a wire-tap; passes the message through unchanged. |

## Integration

| Block | Key settings |
| --- | --- |
| `rest` | `connector`, `method`, `path` (both **static**), `query`/`headers` (maps of CEL), `body` (CEL), `bodyType` (`raw`\|`multipart`), `failOnError`, `statusVar`. |
| `rest-dynamic` | Same, but `method`, `path`, `query`, `headers`, `body` are **all** CEL, and `query`/`headers` are one expression evaluating to a whole map. Use when the endpoint itself is data. Also takes `allowMethods` and `pathPrefix`. |
| `flow-ref` | `flow`, `oneWay` — invoke another flow by name. |
| `cli-run` | `program` (CEL, absolute path), `args` (CEL list), `allow` (absolute paths; required when `program` depends on the message), `env`, `workDir`, `timeout`, `onExit`, `events`/`emit` (stdout/stderr/exit). Runs a local program; no shell, argv only. |
| `jwt-validate` | Filter: verify a bearer JWT against an OIDC provider; claims land in `vars.jwt`. |
| `sse-event` | `event`, `data`, `close`, `ifClosed` — write one frame to the caller's open stream. Requires an SSE route. |
| `publish-event` | `subject` (CEL), `value` — broadcast to a topic. |
| `queue-dispatch` | `subject` — send to a queue for competing consumers. |

To call an API **as the caller** rather than as the deployment, set the
`Authorization` header on the block -- one entry in `rest`'s `headers` map, or one
key of `rest-dynamic`'s rendered map. The connector applies its own `auth` only
when the request carries none, so a block that sets the header uses its own
credential and one that does not gets the connector's.

```yaml
# route declares `headers: [Authorization]`, so the token is in vars
- type: rest
  settings:
    connector: upstream
    path: /v1/me
    headers:
      Authorization: 'vars["Authorization"]'
```

To send a file, set `bodyType: multipart` and let `body` evaluate to a parts map -- the same shape `body.parts` holds inbound, so forwarding an upload is `body: 'body.parts'`. Build one with `multipart()` and `.addPart(name, value)`; a scalar is a text field, an object may set `data`, `encoding`, `filename`, `contentType`. The block generates the boundary and sets `Content-Type` itself, so do **not** set that header:

```yaml
- type: rest
  settings:
    connector: upstream
    method: POST
    bodyType: multipart
    body: |
      multipart()
        .addPart("caption", body.caption)
        .addPart("avatar", body.parts.avatar)
```

## Storage

| Block | Key settings |
| --- | --- |
| `object-read` | Read from the runtime store into the body or a variable. `volatile` picks the tier. |
| `object-write` | Write an object under a key. `volatile` picks the tier. |
| `object-delete` | Delete by key. `volatile` picks the tier. |
| `invalidate-cache` | Evict a `cache-scope` entry by key. |

The storage blocks operate in one of two tiers, chosen by `volatile` (default
`false`). The persistent tier survives a restart — Postgres on the platform, a
serialized file standalone. The volatile tier does not promise to: Redis on the
platform (with LRU eviction and no persistence), process memory standalone, and
either may drop the value. Use volatile
for state whose loss costs a recompute (a counter, a scratch value) and persistent
for anything a flow relies on finding.

The tiers are separate keyspaces, so a read must name the same tier its write used;
reading the other one is a clean miss. `cache-scope` and `invalidate-cache` are
always volatile and have no setting for it.
| `file-read` | `connector`, `path` (CEL), `encoding` (`text`\|`base64`), `contentType`, `resultVar`. Reads a file under the connector's root; a path that leaves the root, or an absolute one, is refused. |
| `file-write` | `connector`, `path` (CEL), `content` (CEL; empty writes the body), `encoding`, `resultVar` → `{path, bytes}`. Creates parent directories when the connector sets `createDirs`. Passes the message through. |
| `sql` | `connector`, the statement, and parameters — against a `database` connector. |

## AI (leaves)

| Block | What it does |
| --- | --- |
| `ai-mapping` | Reshape the body to a target shape described by a prompt, validated against a schema. |
| `ai-embed` | Text to embedding vectors, into a variable. |
| `clear-agent-memory` | Erase one `ai-agent` thread's stored memory by thread id. |

## Vendor blocks

**Notion:** `notion-verify-request`, `notion-event`, `notion-retrieve-page`,
`notion-retrieve-blocks`, `notion-query-datasource`, `notion-page-to-markdown`.

**Slack:** `slack-verify-request`, `slack-event`, `slack-send-message`,
`slack-update-message`, `slack-add-reaction`, `slack-lookup-user`.

**MongoDB:** `mongodb-find`, `mongodb-insert`, `mongodb-update`, `mongodb-delete`,
`mongodb-aggregate`.

**Pinecone:** `pinecone-query`, `pinecone-upsert`, `pinecone-fetch`,
`pinecone-delete`.

**Tavily (agentic web search):** `tavily-search`, `tavily-extract`,
`tavily-crawl`, `tavily-map`. Each returns its response as the **body** unless
`resultVar` names a variable. `tavily-crawl` and `tavily-map` run server-side for
up to 150s, so the connector's `timeout` must be raised past its 30s default.
**Parallel (web research):** `parallel-search`, `parallel-extract`,
`parallel-task-run`, `parallel-verify-request`. `parallel-search` answers in the
request and returns its response as the **body**. `parallel-extract` reads the
`urls` it is given — excerpts scoped to an optional `objective`, or the whole
page with `fullContent` — and likewise returns its response as the **body**.
`parallel-task-run` does not answer: it starts an
asynchronous run and puts the handle in `vars.parallelRun`, leaving the body
alone — the result arrives later as a signed webhook, which
`parallel-verify-request` authenticates over the raw request bytes.

## The ai-agent block

```yaml
- type: ai-agent
  name: assistant
  connector: claude          # slot: an llm-category connector name
  prompt: >                  # slot
    What the agent is for.
  guardrail: >               # slot, optional — when to take the default path
    If the question is not about orders, take the default path.
  input: body.message        # CEL — the opening user turn; unset sends the whole body
  answer: text               # json (default) or text — see below
  maxIterations: 8           # slot, default 8
  stream: true               # token-level streaming; needs an events path
  emit: [text, tool_call, tool_result, done]
  memoryThreadId: body.threadId    # CEL — one conversation per thread. A transcript is
                                   # keyed by this and nothing else, so two agents
                                   # resolving the same id share (and overwrite) one
                                   # conversation. An ai-agent inside another one's tool
                                   # slot should use `vars.toolScope` — the scope the
                                   # runtime mints for that branch — or no memory at all.
  memoryVolatile: true             # transcripts in the volatile tier (Redis), for a
                                   # conversation whose loss costs nothing. Pairs with
                                   # vars.toolScope for a nested agent.
  contextMaxTokens: 8000           # the whole prompt's budget. memoryMaxTokens is the
                                   # name this replaced and is REJECTED at build time.
  memoryCompaction: summarize      # or prune
  events:                    # slot: a sub-flow run once per agent event
    process:
      - type: sse-event
        settings: { event: agent, ifClosed: stop }
  tools:                     # slot: each tool is a sub-flow. A branch runs on the
                             # agent's message and is told about its call:
                             # vars.toolScope (stable per tool per conversation),
                             # vars.toolName, vars.toolCallId (unique per call).
    - name: lookup_order
      description: Look up an order by id.
      inputSchema: |
        { "type": "object", "required": ["orderId"],
          "properties": { "orderId": { "type": "string" } } }
      process:
        - type: set-payload
          settings: { value: '{"status": "shipped"}' }
  skills:                    # slot: instructions loaded on demand
    - name: refunds
      description: How to decide and explain refunds.
      resource: refunds      # a template resource alias
  default:                   # slot: taken when the guardrail trips
    process:
      - type: set-payload
        settings: { value: '{"answer": "Let me get a human."}' }
```

A tool's arguments arrive **as the message body**, and the branch's output body is
returned to the model as the result. A branch error becomes an error *result* fed
back to the model rather than aborting the agent.

`answer` decides one sentence of the system prompt. The default, `json`, tells the
model to reply as JSON only — right when the agent's answer becomes the next
block's body, since that is what a later `body.tier` depends on. Use `answer: text`
whenever a **person** reads the reply: without it the agent is told to answer in
JSON here and in prose by its own prompt, and which instruction it follows is the
provider's choice rather than yours. The reply is parsed the same way either way —
JSON becomes a structured body, anything else stays a string.

`input` is the opening user turn. Unset, the model is handed the whole input body
as a JSON document, which is what an agent transforming a payload wants; a chat
agent sets it to the question so the rest travels as labelled context.

Agent event types: `turn_start`, `text`, `thinking`, `tool_input`, `custom`,
`tool_call`, `tool_result`, `turn_end`, `guardrail`, `done`, `error`. A type left
out of `emit` is never built at all, which on a token stream matters.
