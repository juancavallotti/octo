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
| `file` | File | no | `file-read`, `file-write` |
| `mongodb` | MongoDB | no | `mongodb-*` |
| `pinecone` | Pinecone | no | `pinecone-*` |
| `notion` | Notion | no | `notion-*` |
| `tavily` | Tavily | no | `tavily-*` |
| `parallel` | Parallel | no | `parallel-*` |
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
    methods: [GET, POST]        # omit to answer every method
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

Naming `methods` makes the router answer every other method with `405` before any
flow runs; omitting it accepts all of them, and you filter with `vars.method` in a
`condition`. Listing `GET` also answers `HEAD`.

A **`multipart/form-data`** request is decoded automatically -- no setting, and
`rawBody` does not disable it. Its parts land on `body.parts`, keyed by part name:

```yaml
value: |
  {
    "who":  body.parts.username.data,
    "file": body.parts.avatar.filename,
    "type": body.parts.avatar.contentType,   # each part has its OWN content type
    "size": body.parts.avatar.size           # always the decoded byte length
  }
```

A part with a `filename` has `encoding: base64` (read it with
`base64.decode(part.data)`); a plain field is `text`. `contentType` and `rawData`
are untouched underneath, so a signature over the raw body still verifies. Raise
`maxBodyBytes` for uploads -- the 1 MiB default is small, and the body holds both
the raw payload and the parts.

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
      type: bearer              # none | bearer | basic | oauth2 | gcp
      token: ${API_TOKEN}
    retry: { maxAttempts: 3 }
    cache: { enabled: true, ttl: 60s }
```

`baseURL` is a hard boundary: a block's path cannot change the host. The
deployment's own credential belongs in `auth`. A credential the flow obtains at
runtime -- a token relayed from the caller -- goes on the block's `Authorization`
header instead; the connector applies its own `auth` only when the request carries
none, so the two do not contend.

**`type: gcp`** authenticates as the service account the runtime runs as, using
the GCP metadata server -- no secret to configure. Use it for a runtime on Cloud
Run, GCE, or GKE calling another Cloud Run service or a Google API:

```yaml
    auth:
      type: gcp                 # identity token; audience defaults to baseURL
```

```yaml
    auth:
      type: gcp
      gcpToken: access          # for calling Google APIs directly
      gcpScopes: [https://www.googleapis.com/auth/devstorage.read_only]
```

It only works where a metadata server exists. Drive `type` from an env var --
`type: ${API_AUTH}`, empty locally and `gcp` in production -- rather than
expecting it to work on a laptop. An empty `type` means no auth, which makes this
the general pattern for turning auth off in local development.

## database

```yaml
- name: orders-db
  type: database
  settings:
    driver: postgres            # or sqlite, mysql
    dsn: ${DATABASE_URL}
```

## file

```yaml
- name: workspace
  type: file
  settings:
    root: /workspace            # required; no default, and it must exist at startup
    createDirs: false           # create missing parent directories when writing
```

The root is the boundary, and it is enforced by the kernel rather than by string
matching: paths resolve through `os.OpenRoot`, so a symlink inside the root cannot
name a file outside it. An absolute path is refused outright rather than folded back
under the root, because `<root>/etc/passwd` is contained but is not the file that was
named. Files are created 0600, directories 0700.

A missing root fails the deployment at startup, not the first message — so a `file`
connector is a claim that the directory is there.

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
