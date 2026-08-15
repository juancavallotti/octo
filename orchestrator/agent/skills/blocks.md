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
| `rest` | `connector`, `method`, `path` (both **static**), `query`/`headers` (maps of CEL), `body` (CEL), `failOnError`, `statusVar`. |
| `rest-dynamic` | Same, but `method`, `path`, `query`, `headers`, `body` are **all** CEL, and `query`/`headers` are one expression evaluating to a whole map. Use when the endpoint itself is data. Also takes `allowMethods` and `pathPrefix`. |
| `flow-ref` | `flow`, `oneWay` — invoke another flow by name. |
| `cli-run` | `program` (CEL, absolute path), `args` (CEL list), `allow` (absolute paths; required when `program` depends on the message), `env`, `workDir`, `timeout`, `onExit`, `events`/`emit` (stdout/stderr/exit). Runs a local program; no shell, argv only. |
| `jwt-validate` | Filter: verify a bearer JWT against an OIDC provider; claims land in `vars.jwt`. |
| `sse-event` | `event`, `data`, `close`, `ifClosed` — write one frame to the caller's open stream. Requires an SSE route. |
| `publish-event` | `subject` (CEL), `value` — broadcast to a topic. |
| `queue-dispatch` | `subject` — send to a queue for competing consumers. |

## Storage

| Block | Key settings |
| --- | --- |
| `object-read` | Read from the runtime store into the body or a variable. |
| `object-write` | Write an object under a key. |
| `object-delete` | Delete by key. |
| `invalidate-cache` | Evict a `cache-scope` entry by key. |
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
  memoryThreadId: body.threadId    # CEL — one conversation per thread
  memoryMaxTokens: 8000
  memoryCompaction: summarize      # or prune
  events:                    # slot: a sub-flow run once per agent event
    process:
      - type: sse-event
        settings: { event: agent, ifClosed: stop }
  tools:                     # slot: each tool is a sub-flow
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
