# Connectors

The complete registry, as the running binary reports it. A connector not listed here
does not exist.

| Type | Label | Provides a source | Blocks that bind to it |
| --- | --- | --- | --- |
| `http` | HTTP Server | yes — HTTP route | none |
| `http-client` | HTTP Client | no | `rest`, `rest-dynamic` |
| `cron` | Cron | yes — schedule | none |
| `database` | Database | no | `sql` |
| `queue` | Platform Queue | yes — queue subscription | `queue-dispatch` |
| `events` | Platform Events | yes — event subscription | `publish-event` |
| `logger` | Logger | no | `log` |
| `mongodb` | MongoDB | no | `mongodb-*` |
| `pinecone` | Pinecone | no | `pinecone-*` |
| `notion` | Notion | no | `notion-*` |
| `slack` | Slack | yes — events | `slack-*` |
| `llm-anthropic` | Anthropic | no | AI blocks, by category |
| `llm-openai` | OpenAI | no | AI blocks, by category |
| `llm-gemini` | Gemini | no | AI blocks, by category |

`queue` and `events` are **implicit**: they take no settings and a source resolves
one by type on demand, so a flow using them usually declares no connector entry.

## http

```yaml
- name: api
  type: http
  settings:
    host: ${HTTP_HOST}          # default 0.0.0.0
    port: ${HTTP_PORT}          # default 8080
    basePath: /v1
    requestTimeout: 30s
    cors:
      allowedOrigins: ["https://example.com"]
```

Its **source**:

```yaml
source:
  connector: api
  type: http
  settings:
    path: /orders/{id}          # path params land in vars
    headers: [X-Request-Id]     # listed headers land in vars
    timeout: 30s
    maxBodyBytes: 1048576
    rawBody: false
    sse:
      enabled: true
      finalEvent: true
      finalEventName: answer
      heartbeat: 15s
      maxDuration: 5m
```

The request maps in as: path params to `vars.<name>`, `vars.method`, `vars.query`
(a map), listed headers to `vars.<Header-Name>`, and the JSON body to `body`. Set
`vars.httpStatus` to control the response code.

There is **no `methods` setting** — a route accepts every method, and you filter
with `vars.method` in a `condition`.

With `sse.enabled`, the stream address lands in `vars.sseStream` and `sse-event`
finds it with no configuration. Nothing is written until the first frame, so a
`validate` in front of the stream still returns an ordinary error response.

## http-client

```yaml
- name: payments
  type: http-client
  settings:
    baseURL: https://api.example.com
    timeout: 30s
    headers: { X-App: octo }
    maxResponseBytes: 1048576
    auth:
      type: bearer              # none | bearer | basic | oauth2
      token: ${API_TOKEN}
    retry: { maxAttempts: 3 }
    cache: { enabled: true, ttl: 60s }
```

`baseURL` is a hard boundary: a block's path cannot change the host. Credentials
belong in `auth`, not in a block's headers.

## database

```yaml
- name: orders-db
  type: database
  settings:
    driver: postgres            # or sqlite, mysql
    dsn: ${DATABASE_URL}
```

## cron

```yaml
- name: ticker
  type: cron
# source:
#   connector: ticker
#   type: cron
#   settings: { schedule: "@every 30s" }
```

## LLM connectors

All three share `apiKey`, `model`, `maxTokens` and `baseURL`. AI blocks reference
one by name; the block's `connector` slot resolves against the `llm` category, so
any of the three works.

```yaml
- name: claude
  type: llm-anthropic
  settings:
    apiKey: ${ANTHROPIC_API_KEY}
    model: claude-sonnet-4-6
    maxTokens: 8192
    thinking: adaptive          # off | adaptive | budgeted
    thinkingBudget: 4096        # when budgeted
```

OpenAI takes `reasoning` (`off|minimal|low|medium|high`) instead of `thinking`;
Gemini takes `thinking` (`off|dynamic|budgeted`) and `thinkingBudget`.

Keep keys out of the file: declare them under `env:` and reference `${NAME}`.

## queue and events

```yaml
# Competing consumers — each message handled once across replicas.
- type: queue-dispatch
  settings: { subject: orders }

# Broadcast — every subscriber on the subject receives every message.
- type: publish-event
  settings:
    subject: '"notifications"'
    value: 'body'
```

Both are scoped to the deployment, so two deployments using the same subject name
never hear each other. A subject written with a leading `system:` opts out of that
scoping and reaches the platform's own subjects — publish only, never subscribe.
