# The flow file

An integration is one YAML document (or a directory of them, merged). Load this
before writing or editing a definition — the schema moves faster than any model's
training data, and a definition that does not load is worse than saying you are
unsure.

## Top-level keys

```yaml
service:     # identity — required
env:         # declared environment variables
resources:   # env files and templates
connectors:  # connector instances flows reference by name
processors:  # reusable named block definitions
flows:       # the pipelines
```

Only `service` is required, and only `service.name` within it. `service.environment`
is a free-form label.

## env

A variable **must** be declared here before any value may reference it as
`${NAME}`. Referencing an undeclared one fails the load.

```yaml
env:
  - name: HTTP_PORT
    default: "8080"
  - name: NOTION_TOKEN
    required: true      # a default does NOT satisfy required
```

Resolution: OS environment > `.env` file > `default`.

Substitution rewrites **every value in the document** — settings at any depth, a
block's `condition`, `workers`, `service.name`, even a connector's `type`. The two
exceptions are the `env:` and `resources:` sections themselves, which are read to
build the environment in the first place.

A value that is *exactly* one placeholder takes the variable's natural YAML type, so
`port: ${HTTP_PORT}` fills an integer field. A placeholder inside a longer string
stays a string.

In CEL, resolved variables are readable as `env.NAME`. **Inside an expression
setting, prefer `env.NAME` over `${NAME}`** — a substituted value lands as raw CEL
source, so `database: '"${DB_ID}"'` is needed to produce a string literal.

## resources

```yaml
resources:
  env:
    - .env.dev                    # overlays the .env chain
  templates:
    - resource: templates/welcome.tmpl
      as: welcome                 # the alias blocks and skills reference
```

Paths resolve against the config's directory. On the platform, resources are rows on
the integration rather than files — same aliases, same behaviour.

## connectors

Each entry instantiates one connector type under a unique name. Flows reference the
**name**, so one type can be instantiated several times with different settings.

```yaml
connectors:
  - name: api
    type: http
    settings:
      port: ${HTTP_PORT}
  - name: orders-db
    type: database
    settings:
      driver: sqlite
      dsn: file:orders.db
```

## flows

```yaml
flows:
  - name: submit-order        # unique; flow-ref and invoke call it by this name
    source:                   # optional — omit for a flow callable by name
      connector: api          # the connector NAME, not its type
      type: http              # a source type that connector provides
      settings:
        path: /orders
    process:                  # required: the block chain, in order
      - type: log
        settings: { message: '"received " + toJson(body)' }
    error:                    # optional, root flows only
      - type: set-payload
        settings: { value: '{"error": vars.error.message}' }
    workers: 8                # optional, default 8
    buffer: 64                # optional, default 64
    pool: 8                   # optional, default 8 — shared by concurrent composites
```

A flow with **no source** gets an implicit one: it binds nothing external and is
callable by name, from `octo invoke`, a `flow-ref` block, or an `ai-agent` tool.

`error` runs when `process` returns an error, with the failure exposed as
`vars.error` (`message`, `flow`, `block`). If the error chain succeeds its output
becomes the flow's result — that is recovery. For an HTTP flow, set
`vars.httpStatus` to control the response code.

`source`, `error`, `workers`, `buffer` and `pool` are **root-flow only**. A sub-flow
inside a composite may declare only `name` and `process`.

## Blocks

Every block entry shares four fields: `type` (or `ref`), `name`, `settings`.

**Leaf blocks** put everything under `settings`.

**Composite blocks** additionally use typed *top-level* keys called slots, which sit
on the block entry itself and **not** under `settings`:

```yaml
- type: if
  name: any-orders
  condition: "size(body.orders) > 0"   # slot: top-level
  then:                                # slot: a sub-flow
    process:
      - type: log
        settings:                      # leaf settings stay under settings
          message: '"processing orders"'
```

Getting this wrong is the most common mistake, and it fails the build with a precise
message: `block "log" is a leaf and must not declare composite slots [condition]`.

The slot vocabulary: `process`, `error`, `branches`, `condition`, `then`, `else`,
`cases`, `default`, `items`, `as`, `mode`, `body`, `setBody`, `setVars`, `key`,
`ttl`, `rules`, `onReject`, `rejectStatus`, `connector`, `prompt`, `guardrail`,
`routes`, `tools`, `skills`, `maxIterations`, `maxAttempts`, `serverName`,
`resources`, `prompts`, `memoryThreadId`, `memoryMaxTokens`, `memoryCompaction`.

## processors

Reusable named block definitions, referenced with `ref`. A referencing block takes
its type and base settings from the definition; its own `settings` override
key-by-key. A block sets `ref` or `type`, not both.

```yaml
processors:
  - name: audit
    type: log
    settings:
      logger: out
      message: '"processed " + eventID'

flows:
  - name: example
    process:
      - ref: audit
      - ref: audit
        settings: { message: '"done: " + eventID' }
```

## Files

`--config` may point at a directory: every `.yaml`/`.yml` directly inside it is
merged, sorted by name, subdirectories ignored. Files ending `_test.yaml` are test
suites, not config, and are skipped by the loader.
