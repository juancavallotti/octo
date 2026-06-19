# Processing Pipeline

This document describes the runtime building blocks that turn connector events
into messages and process them concurrently. It covers the conceptual model, the
configuration schema, the concurrency model, and the start/stop lifecycle.

> **Status.** The structure and the composite *execution model* are now defined:
> processing is a hybrid of single-threaded composition and opt-in concurrency
> (see [Execution model](#execution-model)). `scope` runs sequentially; `fork`
> runs its branches concurrently on a flow-owned worker pool. Still deferred:
> multi-output processors (what a block returns when it emits more than one
> message), fire-and-forget stages, cross-composite backpressure, and the `loop`
> composite.

## Concepts

```
connector --> source --> flow --> block --> processor
                          (worker pool runs the flow per message)
```

- **Message** (`types.Message`) — the first-class unit of work. JSON-only body,
  per-message `Variables`, a stable `EventID`, and an optional `CorrelationID`.
- **Connector** (`core.Connector`) — a runtime component with `Start`/`Stop`. A
  connector owns its own resources (connections, transaction managers).
- **MessageSource** (`core.MessageSource`) — a flow's entry point, **created and
  owned by a connector** via the optional `core.SourceProvider` capability. The
  source responds to connector events by building a `*types.Message` and sending
  it on a channel. Because the connector builds the source, the source closes
  over the connector's resources — so there is **no separate globals registry**.
- **MessageProcessor** (`core.MessageProcessor`) — the processing abstraction:
  `Process(ctx, *Message) (*Message, error)`. Returning `(nil, nil)` drops the
  message (filter); a non-nil error aborts it.
- **Block** (`core.Block`) — a configured, named stage wrapping one processor.
- **Flow** (`core.Flow`) — an ordered list of blocks. A `Flow` is **itself a
  `MessageProcessor`**, which makes it the recursive composition unit: composite
  blocks embed sub-flows.

## Composite blocks

Composition is recursive: a `Flow` contains blocks, and a composite block embeds
sub-flows. Composite kinds use **explicit typed slots**, so the YAML schema is
self-documenting and the builder knows each kind's shape:

- **`scope`** — an error/transaction boundary. Slots: `main` (the protected flow)
  and optional `alternative` (the catch / recovery / compensation flow). Runs
  **sequentially** (`main`, then `alternative` on failure).
- **`fork`** — a scatter / multi-branch block. Slot: `branches` (an array of
  flows). Runs its branches **concurrently** on the flow's shared pool, each
  branch operating on its own `msg.Clone()`; it joins before returning and passes
  the input message through unchanged. The first branch error aborts the fork and
  cancels the rest.

The flow builder **dispatches on block type**: composite kinds build their typed
sub-flows directly; every other (leaf) block type is resolved through the
`core.BlockRegistry`. Leaf blocks self-register via `core.MustRegisterBlock` in
an `init` function, the same pattern connectors use.

> Adding a new composite *kind* (e.g. `loop`) means extending the builder and the
> config, not just registering a factory. This is the accepted cost of explicit
> typed slots while the set of composite kinds is small.

## Execution model

Processing is a **hybrid of single-threaded composition and opt-in concurrency**.
The composition seam, `MessageProcessor.Process(ctx, *Message) (*Message, error)`,
is synchronous and one-in / one-out, so a `Flow` runs its blocks in order — the
simple, single-threaded path. A composite block may *opt into* concurrency
internally and **join before it returns**, keeping the seam (and the one-terminal-
event-per-message guarantee) intact. `scope` proves the simple half (sequential);
`fork` proves the concurrent half.

### Two levels of concurrency

1. **Per-flow worker pool.** Each top-level flow is run by a **dedicated pool of
   worker goroutines** all reading the same channel the source emits on (`workers`,
   `buffer`). A worker takes a message and runs it through the root block chain.
2. **Shared flow pool.** Each flow also owns a **single shared worker pool**
   (`pool`) that is started with the flow and threaded down through the build, so
   composite blocks that parallelize (e.g. `fork`) schedule work on it instead of
   each spawning its own goroutines. The pool is started before the source emits
   and stopped after the per-flow workers drain.

- **No cross-flow ordering.** With more than one worker, messages may complete out
  of order. Set `workers: 1` for FIFO processing within a flow.
- **Backpressure** comes from the bounded source channel (`buffer`): when workers
  fall behind, the channel fills and the source blocks.
- A failing message aborts only that message — the worker survives poison
  messages and keeps processing.
- **Pool exhaustion.** The shared pool has a bounded task queue. If a composite
  submits more work than the pool can accept (e.g. deeply nested forks), the pool
  is exhausted and **panics** rather than risk a silent deadlock. Size `pool` for
  the flow's fan-out. This is a deliberate limitation of the current model.

## Flow events

The runtime publishes lifecycle events on a process-wide pub/sub bus
(`core.EventBus`, `core.DefaultEventBus`). Each message produces a `started`
event followed by exactly one terminal event: `completed`, `dropped`, or
`failed` (`types.FlowEvent`). Subscribe with `core.DefaultEventBus().Subscribe`
to observe success and error outcomes (metrics, dead-lettering, etc.).

## Lifecycle

The `core.Service` owns the start/stop lifecycle and the acquire/release
discipline:

1. Build the event bus.
2. Start connectors in config order (each acquires its own resources).
3. Build each flow: resolve its source's connector, ask it for a source, and
   build the root block chain (recursing composite sub-flows).
4. Start each flow: spawn its worker pool, then start the source.
5. On `ctx.Done()`, stop in strict reverse: per flow `source.Stop` → close the
   channel → drain workers; then stop connectors in reverse.

The runtime creates the source's channel and closes it during teardown — **after**
stopping the source — following "whoever creates the channel closes it".

## Configuration

Flows live under the top-level `flows:` key. A root flow binds a `source`, a
worker-pool size, and a shared-pool size; sub-flows nested inside composite blocks
must not declare `source`, `workers`, `buffer`, or `pool`.

```yaml
service:
  name: orders
  environment: prod

connectors:
  - name: orders-kafka
    type: kafka
    settings:
      brokers: ["b1:9092"]

flows:
  - name: ingest-orders
    workers: 8          # per-flow worker pool size; defaults to 1 (FIFO)
    buffer: 128         # source -> worker channel depth; defaults to 64
    pool: 16            # shared pool for concurrent composites; defaults to 8
    source:
      connector: orders-kafka   # references connectors[].name
      type: topic               # interpreted by the connector
      settings:
        topic: orders
    process:
      - type: validate
        settings:
          schema: order.schema.json
      - type: scope               # composite: error/transaction boundary
        name: persist
        main:                     # protected flow
          process:
            - { type: transform, name: normalize }
            - { type: pg.upsert, settings: { table: orders } }
        alternative:              # catch / recovery / compensation flow
          process:
            - { type: deadletter }
      - type: fork                # composite: parallel branches
        name: notify-and-audit
        branches:
          - { name: notify, process: [ { type: email } ] }
          - { name: audit,  process: [ { type: log } ] }
```

### Named processors (`ref`)

Reusable processor definitions live under the top-level `processors:` key, the
same way connectors are declared once and referenced by name. Each definition has
a `name`, a `type`, and `settings`. A flow block then references one with `ref`
instead of an inline `type`:

```yaml
processors:
  - name: audit-log
    type: log
    settings:
      level: info
      message: '"order " + body.id + " received"'

flows:
  - name: ingest
    source: { connector: ticker, type: cron, settings: { schedule: "@every 5s" } }
    process:
      - ref: audit-log                      # reuse the named definition
      - ref: audit-log                       # ...as many times as needed
        settings: { level: debug }           # block settings override the ref, key-by-key
```

A block sets **either** `ref` **or** `type` (an inline `type` equal to the
referenced type is the one allowed overlap). When `ref` is set, the block's own
`settings` are shallow-merged over the referenced settings, so a shared definition
can be tuned per use.

### Settings

Both `connectors[].settings` and a block's effective settings are a
`types.Settings` map. A component reads them by projecting the whole map onto its
own typed struct with `Settings.Decode(&cfg)` (mirroring `Message.DecodeBody`), so
each connector and processor owns its settings shape and a mistyped value fails at
startup. Typed accessors (`String`, `Int`, `Bool`, `Float`) are available for
one-off reads.

### Expressions (CEL)

Expressions use [CEL](https://github.com/google/cel-go), wrapped by
`core/expr`. An expression is compiled once at flow-build time (a malformed
expression fails at startup) and evaluated per message against an **activation** —
a map of variable names to values. Results come back as JSON-native Go values, so
they slot straight into a message body. Each call site decides which variables it
exposes:

- **`log` block** (`message` setting) sees `body`, `vars`, `eventID`,
  `correlationID`. With no `message` it logs the JSON body. `level` is the level
  this line is emitted at (`debug`/`info`/`warn`/`error`, default `info`). Setting
  `full: true` additionally attaches the whole message (correlation id, variables,
  body, schema) as structured attributes for debugging — pair it with a `json`
  logger for a clean dump. It is a pass-through wire tap: it logs and forwards the
  message unchanged.
- **`cron` source** (`payload` setting) sees `now` (the fire time) and the
  source's static `settings`. The result becomes the message body.

### The `log` block and `logger` connectors

The `log` block writes through a logger. By default — no `logger` set — it uses
the process default logger. To control the output, declare a `logger` connector
and reference it by name. The connector **owns its output**: it opens a file on
start and closes it on shutdown, following the same acquire/release discipline as
any other connector.

Logger settings are the common slog knobs, and every one defaults, so a logger
can be declared with no settings at all (`output: stdout`, `format: text`,
`level: info`):

| setting     | values                          | default  |
| ----------- | ------------------------------- | -------- |
| `output`    | `stdout`, `stderr`, a file path | `stdout` |
| `format`    | `text`, `json`                  | `text`   |
| `level`     | `debug`/`info`/`warn`/`error`   | `info`   |
| `addSource` | `true`/`false`                  | `false`  |

A logger's `level` is the **minimum** level it emits; the `log` block's own
`level` is the level it emits each line **at**. A block reaching a named logger
connector is the general pattern for blocks that depend on a connector capability:
the flow builder hands each block factory a resolver (`core.BlockDeps`) so it can
look up a connector by name and use the capability it provides (here, a logger).

```yaml
connectors:
  - { name: ticker, type: cron }
  - name: audit
    type: logger
    settings:
      output: /tmp/octo-audit.log
      format: json

flows:
  - name: audit-ticks
    source: { connector: ticker, type: cron, settings: { schedule: "@every 2s", payload: '{"date": string(now)}' } }
    process:
      - type: log
        settings:
          logger: audit                       # write through the named logger
          message: '"tick at " + body.date'
```

### The `cron` source

The `cron` connector emits a message on a schedule. Seconds are enabled, so a
standard expression is **six fields** (`sec min hour dom mon dow`) and descriptors
like `@every 2s` also work. Settings: `schedule` (required), `payload` (optional
CEL expression for the body), and `correlationID` (optional).

```yaml
connectors:
  - { name: ticker, type: cron }

flows:
  - name: greet
    source:
      connector: ticker
      type: cron
      settings:
        schedule: "0,30 * * * * *"          # second 0 and 30 of every minute
        payload: '{"date": string(now)}'
    process:
      - { type: log, settings: { message: '"hello world! the date is " + body.date' } }
```

Runnable samples live under [`samples/`](../samples); run one with
`task run:sample -- hello-world.yaml` (or the **Debug sample** launch config in
VS Code).

### The `http` connector and source

The `http` connector turns synchronous HTTP requests into flow executions and
returns the result to the caller. The **connector** owns one HTTP server; its
**sources** register routes on it. A request builds a message, the flow runs, and
the **final message body is written back as JSON** — so an HTTP source is
request/response, unlike the fire-and-forget `cron` source.

Connector settings (the HTTP server, all optional):

| setting          | meaning                                   | default        |
| ---------------- | ----------------------------------------- | -------------- |
| `host`           | bind address                              | all interfaces |
| `port`           | bind port (`0` = OS-assigned)             | `8080`         |
| `basePath`       | prefix prepended to every source path     | none           |
| `keepAlive`      | enable HTTP keep-alives                   | Go default     |
| `requestTimeout` | how long a handler waits for the flow     | `30s`          |
| `readTimeout` / `writeTimeout` / `idleTimeout` | server timeouts             | unset          |

Source settings (one route bound to the flow):

| setting               | meaning                                                    | default |
| --------------------- | ---------------------------------------------------------- | ------- |
| `path`                | route pattern, e.g. `/orders/{id}` (required)              | —       |
| `headers`             | request headers to copy into variables                     | none    |
| `correlationIdHeader` | header to source the message `CorrelationID` from          | none    |
| `timeout`             | per-route wait for the flow                                | connector `requestTimeout` |
| `maxBodyBytes`        | request body size cap                                       | 1 MiB   |

The route catches **all methods**, so content-based routing is done in-flow with
`switch`/`if` against the variables the source sets:

- **path params** → top-level vars (`/orders/{id}` → `vars.id`),
- **`vars.method`** → the HTTP method,
- **`vars.query`** → always a map (empty when there is no query string), so
  `has(vars.query.x)` is safe,
- **configured `headers`** → always set (empty string when absent); read them in
  CEL by index, e.g. `vars["X-Tenant"]`, since header names contain dashes.

The JSON request body becomes `body`; a malformed body is rejected with **400**
before the flow runs. The flow outcome maps to the response: **completed → 200**
with the final body, **dropped → 204**, **failed → 500**. A handler that outlives
its `timeout` returns **504**. Correlation rides the flow-event bus: the connector
subscribes once and matches each terminal `FlowEvent` (which now carries the
message in `Result`) back to the waiting request by `EventID`.

> **Status / future work.** This is the foundation, not a complete HTTP stack. A
> logical-error branch (e.g. the `default` case of a method `switch`) still returns
> 200 today; mapping a flow result to an arbitrary HTTP status code is deferred.

```yaml
connectors:
  - name: api
    type: http
    settings: { basePath: /api/v1, port: 8080, requestTimeout: 5s }

flows:
  - name: orders-api
    source:
      connector: api
      type: http
      settings:
        path: /orders/{id}
        headers: [X-Tenant]
        correlationIdHeader: X-Request-Id
    process:
      - type: switch
        name: route-by-method
        cases:
          - when: 'vars.method == "POST"'
            process:
              - { type: set-payload, settings: { value: '{"order": body, "status": "accepted"}' } }
        default:
          process:
            - { type: set-payload, settings: { value: '{"error": "unsupported"}' } }
```

See [`samples/http-orders.yaml`](../samples/http-orders.yaml) for a fuller example
(query-param defaulting, POST-body transform, conditional priority flag).

## Writing a connector source

A connector becomes a source by implementing `core.SourceProvider`. The runtime
hands the source the channel it should emit on; the source runs on its own
goroutine and must not send after `Stop` returns. See
[`runtime/connectors/noop/source.go`](../runtime/connectors/noop/source.go) for a
minimal reference implementation. A source reads its configuration from
`cfg.Settings` (a `types.Settings`) by decoding it into a typed struct.
