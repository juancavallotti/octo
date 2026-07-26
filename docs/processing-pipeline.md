# Processing Pipeline

> **Published version:** the user-facing form of this document lives on the docs
> site at <https://juancavallotti.github.io/octo/runtime/processing-pipeline/>
> (source: `apps/docs/content/docs/runtime/processing-pipeline.mdx`). This file
> remains the contributor-facing deep-dive; keep both in sync when the runtime
> model changes.

This document describes the runtime building blocks that turn connector events
into messages and process them concurrently. It covers the conceptual model, the
configuration schema, the concurrency model, and the start/stop lifecycle.

> **Status.** The structure and the composite *execution model* are now defined:
> processing is a hybrid of single-threaded composition and opt-in concurrency
> (see [Execution model](#execution-model)). `handle-errors` runs sequentially;
> `fork` runs its branches concurrently on a flow-owned worker pool. Still
> deferred: multi-output processors (what a block returns when it emits more than
> one message), fire-and-forget stages, cross-composite backpressure, and the
> `loop` composite.

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

- **`handle-errors`** — an error boundary with recovery. Slots: `process` (the
  protected chain) and `error` (the recovery chain). Runs **sequentially**:
  `process`, then — on failure — `error`, with the error exposed to it as
  `vars.error` (see [Error handling](#error-handling)). Both slots are bare block
  chains, so the block reads as a mini-flow embedded inline.
- **`fork`** — a scatter / multi-branch block. Slot: `branches` (an array of
  flows). Runs its branches **concurrently** on the flow's shared pool, each
  branch operating on its own `msg.Clone()`; it joins before returning and passes
  the input message through unchanged. The first branch error aborts the fork and
  cancels the rest.
- **`enrich`** — a message-enrichment scope. Slot: `body` (the chain to run) plus
  `setBody` (a CEL expression for the new body) and `setVars` (a map of variable
  name → CEL expression). Runs `body` on an **isolated `msg.Clone()`**, then
  enriches the original message from the scope's result: `setBody` (when set)
  becomes the new body and each `setVars` expression becomes a variable — both
  evaluated against the enriched clone, so they reference the scope's output while
  everything else stays isolated. Omitting both runs `body` purely for
  side-effects. A `body` error aborts; a `body` that drops the message drops it too.

The flow builder **dispatches on block type**: composite kinds build their typed
sub-flows directly; every other (leaf) block type is resolved through the
`core.BlockRegistry`. Leaf blocks self-register via `core.MustRegisterBlock` in
an `init` function, the same pattern connectors use.

> Adding a new composite *kind* (e.g. `loop`) means extending the builder and the
> config, not just registering a factory. This is the accepted cost of explicit
> typed slots while the set of composite kinds is small.

### AI agent memory

The `ai-agent` composite is stateless by default: each invocation is an
independent conversation. Set **`memoryThreadId`** (a CEL expression) to give it
per-thread memory backed by the runtime object store (KV): the agent loads that
thread's prior transcript before its run and saves the accumulated transcript
after, so a conversation persists across invocations. Two knobs bound the stored
transcript:

- **`memoryMaxTokens`** — the token budget (estimated with a chars/4 heuristic;
  there is no tokenizer in the runtime). Defaults to `8000`.
- **`memoryCompaction`** — how the transcript is shrunk when it exceeds the
  budget: `prune` (drop the oldest turns, the default) or `summarize` (fold the
  oldest turns into a running summary via one model call, keeping recent turns).

Memory objects live in the user KV namespace under an `agent-memory/<threadId>`
key, isolated from `object-read`/`object-write` keys. The **`clear-agent-memory`**
leaf block erases a thread by its (CEL-resolved) `threadId`; the delete is
idempotent. See `samples/ai-agent-memory.yaml`.

## Execution model

Processing is a **hybrid of single-threaded composition and opt-in concurrency**.
The composition seam, `MessageProcessor.Process(ctx, *Message) (*Message, error)`,
is synchronous and one-in / one-out, so a `Flow` runs its blocks in order — the
simple, single-threaded path. A composite block may *opt into* concurrency
internally and **join before it returns**, keeping the seam (and the one-terminal-
event-per-message guarantee) intact. `handle-errors` proves the simple half
(sequential); `fork` proves the concurrent half.

### Two levels of concurrency

1. **Per-flow worker pool.** Each top-level flow is run by a **dedicated pool of
   worker goroutines** all reading the same channel the source emits on (`workers`,
   `buffer`). A worker takes a message and runs it through the root block chain.
2. **Shared flow pool.** Each flow also owns a **single shared worker pool**
   (`pool`) that is started with the flow and threaded down through the build, so
   composite blocks that parallelize (e.g. `fork`) schedule work on it instead of
   each spawning its own goroutines. The pool is started before the source emits
   and stopped after the per-flow workers drain.

- **No cross-flow ordering.** Flows default to 8 workers, so messages may complete
  out of order. Set `workers: 1` for FIFO processing within a flow.
- **Backpressure** comes from the bounded source channel (`buffer`): when workers
  fall behind, the channel fills and the source blocks.
- A failing message aborts only that message — the worker survives poison
  messages and keeps processing.
- **Pool exhaustion.** The shared pool has a bounded task queue. If a composite
  submits more work than the pool can accept (e.g. deeply nested forks), the pool
  is exhausted and **panics** rather than risk a silent deadlock. Size `pool` for
  the flow's fan-out. This is a deliberate limitation of the current model.

## Error handling

Error handling has two layers, both **recovery**-oriented (a successful recovery
chain's output becomes the result) and both exposing the failure to the recovery
chain as the structured variable `vars.error`:

- **`handle-errors` block** — an inline boundary. Its `process` chain runs; on
  error its `error` chain runs. See [Composite blocks](#composite-blocks).
- **Flow-level `error` path** — a sibling `error:` chain on a root flow. When the
  `process` chain errors, the message is redirected to `error`; on success that
  chain's output becomes the flow's result. Error handling is **optional**: a flow
  with no `error:` chain behaves like an empty handler — the error propagates and
  the flow is reported `failed`.

`vars.error` is a structured object available to a recovery chain (CEL:
`vars.error.message`, `vars.error.flow`, `vars.error.block`):

```jsonc
{
  "message": "block \"charge\": rest request: ... connection refused", // err.Error()
  "flow":    "charge-flowlevel",  // enclosing flow or handle-errors block name
  "block":   "charge"             // failing block label, when recoverable
}
```

For HTTP-sourced flows, a recovery chain can set `vars.httpStatus` to a valid
status code (100–599) and the HTTP source returns it instead of the default 200
— e.g. set `httpStatus` to 502 alongside an error body. See
[samples/error-handling.yaml](../samples/error-handling.yaml) for a runnable
example of both layers, `vars.error`, and `httpStatus`.

## Flow events

The runtime publishes lifecycle events on a process-wide pub/sub bus
(`core.EventBus`, `core.DefaultEventBus`). Each message produces a `started`
event followed by exactly one terminal event: `completed`, `dropped`, or
`failed` (`types.FlowEvent`). Subscribe with `core.DefaultEventBus().Subscribe`
to observe success and error outcomes (metrics, dead-lettering, etc.).

A terminal event carries `Duration` — how long the message took, including its
error path — measured where the flow runs rather than left to a subscriber to
derive by pairing `started` with the terminal. A `failed` event also carries
`Block`, the label of the block the failure came from.

`failed` means **unhandled**. A message its flow's error path recovered is
published as `completed`, because that is what happened to it, so a counter over
`failed` counts what nothing handled rather than everything that went wrong.

## Block events

Flow events are per message; **block events** are per block. Every block the
engine invokes is bracketed by a `pre-invoke` and a `post-invoke`
`types.BlockEvent`, dispatched through `core.BlockEvents`
(`core.DefaultBlockEvents`, or a Service's own via `runtime.WithBlockEvents`).
The two vocabularies join on `EventID`.

An event carries its kind, the block's **path**, the block's type, the message's
`EventID`/`CorrelationID`, and the live message. `post-invoke` fires for all
three outcomes and adds `Duration`, `Err`, and `Dropped`.

### The path is an address

`BlockEvent.Path` is the block's address in the grammar
`<flow>[<chain>].<block>[<branch>].<block>` — the same string
`octo invoke --spies` accepts. The flow builder mints it on the way down
(`engine.builder.path`), the resolver walks it back down the config
(`core/runtime/address.go`), and the editor derives the same string as
`naturalAddress`; the spelling itself lives in one place, `core/blockpath.go`.
`runtime/core/runtime/blockpath_resolve_test.go` pins the round trip.

It is a label, not a handle. Two cases mint one the resolver will not take back:
two blocks it would call ambiguous (two unnamed blocks of the same type in one
chain) share a path, and a name carrying a `.`, `[` or `]` — which nothing
rejects today, at any level — comes out as segments the parser splits the wrong
way. The builder mints anyway in both, because a block that emitted nothing at
all would be worse than one whose label is shared or unparseable. The editor's
`naturalAddress` takes the other choice for the same reason it must: a mock or a
spy keyed by an ambiguous address would resolve to the wrong block.

### Registering a listener

Delivery is inline, always: a listener runs on the flow's own goroutine, before
the block for `pre-invoke` and after it returns for `post-invoke`, and the flow
waits for it. Nothing is queued, so nothing is ever dropped — every watched
invocation is delivered exactly once.

A listener declares the blocks it wants when it registers:

| | `AddSyncFor(paths, fn)` | `AddSync(fn)` |
| --- | --- | --- |
| Receives | only the named block paths | every block in every flow |
| Cost to other blocks | one atomic load and a map lookup | the full event, built for every block |
| For | telemetry on known blocks | debugging, `--metrics-blocks '*'` |

Registering for an empty path set registers nothing: a listener that asked for no
block is not one that wants every block.

Listeners observe. A listener cannot skip or abort a block; the flow's control
flow is the same whether anything is listening or not. A panic propagates into
the flow deliberately — the listener is running as part of it, and swallowing the
panic would hide the bug.

**The message contract.** Every field of a `BlockEvent` except `Message` is
copied on the flow's goroutine and is safe to read anywhere. `Message` is the
live message, and the flow is stopped at this block while the listener runs, so a
listener may read and copy it (`Clone`, `Scoped`, `Reported`). That holds only
while the listener is running: the flow resumes mutating the message as soon as
it returns, so a listener must not retain the pointer. `Variables` is a plain
map, so reading it from another goroutine afterwards is a data race that can
panic the process — `Clone` here and hand the copy to a worker you own.

A listener is called from every goroutine a flow runs on — the flow's worker
pool, and the shared pool for a fork's branches — so it must be safe for
concurrent use, and one that blocks inside a fork branch occupies a pool worker.

**Cost.** Nothing is built, timed, or allocated for a block nobody asked about.
The engine tests the block's path before it makes an event, so the loop pays a
nil check and an atomic load when nothing is registered (~1ns/op, 0 allocs), and
one further map lookup per unwatched block when something is (~9ns/op, 0 allocs).
A watched block pays for its own event and its listeners.

**Why there is no async variant.** There was one — a queue and a drain goroutine
— and it was removed. The producers are every flow worker in the process and the
consumer was a single goroutine, so no buffer size makes the consumer keep up
with sustained load; the depth only sets how long saturation takes to start
dropping. Tail-dropping a full queue is also not sampling, it is "discard during
bursts", which biases telemetry against exactly the traffic worth measuring. For
the work a telemetry listener actually does — an atomic increment — the channel
send being used to avoid it cost more than the work. A listener that genuinely
must do slow or blocking work should `Clone` what it needs and hand the copy to a
worker it owns, so the loss policy is its own rather than the runtime's.

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
worker-pool size, a shared-pool size, and an optional flow-level `error` chain
(see [Error handling](#error-handling)); sub-flows nested inside composite blocks
must not declare `source`, `workers`, `buffer`, `pool`, or `error`.

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
    workers: 8          # per-flow worker pool size; defaults to 8 (set 1 for FIFO)
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
      - type: handle-errors       # composite: error boundary with recovery
        name: persist
        process:                  # protected chain
          - { type: transform, name: normalize }
          - { type: pg.upsert, settings: { table: orders } }
        error:                    # recovery chain; sees vars.error
          - { type: deadletter }
      - type: fork                # composite: parallel branches
        name: notify-and-audit
        branches:
          - { name: notify, process: [ { type: email } ] }
          - { name: audit,  process: [ { type: log } ] }
    error:                        # flow-level error path (root flows only)
      - { type: deadletter }
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

### Resources

The top-level `resources:` key declares the resource files a config imports. Every
resource id is a path resolved **relative to the config directory** (the directory
of the config file, or the directory itself when a directory is loaded), so a
resource lives beside the config or in a subfolder. Two kinds are declared:

```yaml
resources:
  env:
    - .env.dev
    - .env.local
  templates:
    - resource: templates/welcome.tmpl
      as: welcome                       # optional alias
    - resource: templates/footer.tmpl   # unaliased — referenced by its path
```

**`resources.env`** — `.env`-convention files whose values are combined into the
runtime environment, overlaid **in listed order** on top of the built-in `.env` /
`$OCTO_ENV_FILE`. They feed both `${NAME}` substitution and the CEL `env` object,
exactly like the top-level `env:` declarations they supply. The load is lenient: a
**missing** env resource is silently skipped (a missing env file is never an
error), and one that is present but unreadable/unparseable logs a warning and is
skipped. Startup only fails when a **declared, required** variable (a top-level
`env:` entry marked `required: true`) is ultimately unset.

**`resources.templates`** — the template files this config renders (see the
[`template-resource` block](#the-template-resource-block) and the
`templateResource()` CEL function). Each entry names a `resource` (the
template's path) and an optional `as` alias; without an alias the template is
referenced by its path. Unlike env resources, templates are **loaded and parsed at
config load** — the philosophy is to fail on deployment, so a missing or malformed
declared template aborts startup then, rather than when a message first renders it.
See `samples/resources-demo/`.

**Resources in the cloud.** The `resources:` declarations above are the same
everywhere; only *where the bytes come from* differs by how the runtime is run.
Standalone reads each resource from the config directory on disk. In the cloud the
bytes live in the orchestrator: resources are authored per integration (the
integration manager's Resources panel, or the orchestrator's resource API), and
**freeze with the integration when it is tagged** — creating a version tag copies
the current resources into an immutable snapshot alongside the frozen definition,
so a deploy of that tag always ships the resources that matched it. A deployed
runtime then loads each declared resource from the orchestrator by its snapshot
(the `k8s` runtime-services module's resource loader, keyed by `OCTO_SNAPSHOT_ID`)
instead of the filesystem. The runtime never sees the difference: it asks its
resource loader for a resource by kind and id, and the standalone (filesystem) and
cloud (orchestrator) loaders answer the same way.

**Resources in editor and MCP dev runs.** When the editor's RUN button (or an MCP
`run_integration`/`invoke_flow`) starts an integration, it spawns the standalone
`octo` binary through `@octo/run-host`, which writes the config into a
per-namespace run directory. Because the standalone loader roots at the config
directory, run-host first **stages the declared resources into that directory** so
they resolve: the host supplies a `ResourceProvider` (the standalone app reads the
flows dir; the platform fetches from the orchestrator), run-host writes each
declared resource beside the config, and removes them when the run stops. Editing
the resource *list* while a run is live re-stages; a resource's *content* is picked
up on the next Run.

**The `.env.dev` dev-env resource.** The values for a run's declared environment
variables are stored as a special env resource named `.env.dev`, edited in the
editor's "Dev .env" panel. Rather than living in the browser and being injected as
process env, they are persisted to the host's resource store (the standalone flows
dir, the platform's orchestrator DB) so credentials stay server-side — and an MCP
run reads them from the store without the caller supplying anything. run-host
always requests `.env.dev` from the provider and declares it last in the run
config's `resources.env` (so its values override other env resources and declared
defaults, while still yielding to real OS environment variables). It is a dev-run
convenience: `.env.dev` is not part of the saved document, so it never ships in a
cloud snapshot.

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
exposes.

The message-driven blocks — `set-payload`, `set-variable`, `multi-transform`, the
`if`/`switch`/`foreach` guards, and the `rest` block — all see the same surface:
`body`, `vars`, `eventID`, `correlationID`, and `env`. The `env` object holds the config's declared
environment variables resolved to their values (the same ones available for
`${NAME}` substitution), so `env.HTTP_PORT` reads a declared variable at runtime —
e.g. `'"listening on " + env.HTTP_PORT'`. Only **declared** variables appear;
referencing an undeclared key is a CEL no-such-key error.

Every message expression also gets the **`templateResource(id)`** function, which
renders a declared [template resource](#resources) against the current message and
returns the resulting string — e.g. `templateResource("welcome")`. It is registered
once as a message-CEL extension, so it is available at every call site above with
no per-block wiring. The template's own `{{ … }}` inner expressions see the same
`body`/`vars`/`env`/`now` surface.

The **`multi-transform`** block folds a whole chain of `set-payload` /
`set-variable` steps into one: its `transforms` setting is an **ordered list** of
edits, each either `{setBody: <CEL>}` (replace the body) or `{setVar: <name>, value:
<CEL>}` (set a variable). The edits are **additive** — the activation is rebuilt
before each step, so a later expression sees the `body`/`vars` produced by the
earlier ones. See `samples/multi-transform.yaml`.

Other call sites expose their own variables:

- **`log` block** (`message` setting) sees `body`, `vars`, `eventID`,
  `correlationID`. With no `message` it logs the JSON body. `level` is the level
  this line is emitted at (`debug`/`info`/`warn`/`error`, default `info`). Setting
  `full: true` additionally attaches the whole message (correlation id, variables,
  body, schema) as structured attributes for debugging — pair it with a `json`
  logger for a clean dump. It is a pass-through wire tap: it logs and forwards the
  message unchanged.
- **`cron` source** (`payload` setting) sees `now` (the fire time) and the
  source's static `settings`. The result becomes the message body.
- **`queue-dispatch` block** (`subject` setting) sees the same surface as the
  message-driven blocks (`body`, `vars`, `eventID`, `correlationID`, `env`, `now`),
  so a flow can route or shard work per message — e.g. `'"orders." + vars.region'`.
- **`object-read` block** (`key`, `default`, `existsVar` settings) reads an object
  from the runtime store into the body or the `as` variable. On a miss, the
  **`default`** CEL expression — when set — is folded in exactly like a hit (into
  `as`, or the body); with no default a miss nulls the body (body mode) or leaves
  the variable unset. Set **`existsVar`** to also record presence: the block writes
  that variable a boolean (true on a hit), so a flow can branch without a second
  read. When `existsVar` is omitted no such variable is written (the prior
  behavior). See `samples/object-store.yaml`.

### The `template-resource` block

The `template-resource` block renders a declared [template resource](#resources)
against the current message. A template is text with embedded `{{ … }}` CEL
expressions that see the message surface (`body`, `vars`, `eventID`,
`correlationID`, `env`, `now`); the block concatenates the literal spans with each
expression's result. Settings:

| setting  | notes                                                                       |
| -------- | --------------------------------------------------------------------------- |
| `id`     | Required. The template's `resources.templates` alias (its `as`), or its resource path when unaliased. |
| `target` | Optional variable name to store the rendered text in. When omitted, the block replaces the message body (mirroring `set-payload` vs `set-variable`). |

Because the template is a **declared** resource, it is parsed at config load, so a
bad template fails at deployment, not here. The same rendering is available inline
in any CEL expression via `templateResource(id)`.

```yaml
resources:
  templates:
    - resource: templates/welcome.tmpl
      as: welcome

flows:
  - name: greet
    process:
      - type: template-resource
        settings:
          id: welcome           # the alias above (or a resource path)
          target: rendered      # writes vars.rendered; omit to replace the body
```

See `samples/resources-demo/`.

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

A completed flow can **override the status code** by setting `vars.httpStatus` to a
valid code (100–599) — e.g. an [error path](#error-handling) that sets
`httpStatus` to 502 alongside an error body. Invalid or absent values keep the
default 200.

> **Status / future work.** This is the foundation, not a complete HTTP stack.
> Header control on the response is still deferred.

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

### Platform queues: the `queue` source and `queue-dispatch` block

Queues are a **core runtime service** (in-process in the standalone module,
NATS-backed in k8s), scoped to the deployment. Subscribers to a subject form one
**competing-consumer** group, so each message is handled by **exactly one**
replica. That makes queues the construct for **load balancing work across the
cluster**: route work onto a subject and run as many worker replicas as you need —
how integration load balancing happens is up to you. Delivery is at-most-once (a
message published with no live consumer is dropped).

Two blocks expose this to flows — the same "dispatching, or requiring a response"
idiom as `flow-ref`, but across replicas instead of in-process:

- **`queue-dispatch`** (a processor block) sends the current message to a subject.
  By default it **publishes** fire-and-forget and returns the message unchanged —
  dispatching for load balancing is the common case. With `awaitReply: true` it
  instead does a **request**: it waits for one competing consumer's reply and folds
  the reply's `body` and `variables` back into the message (the cross-replica
  analogue of a two-way `flow-ref`). The outgoing message is cloned and **rekeyed**
  so the sub-invocation correlates independently of this flow.

  | setting      | meaning                                                       | default |
  | ------------ | ------------------------------------------------------------- | ------- |
  | `subject`    | CEL expression for the subject to send to (required)          | —       |
  | `awaitReply` | wait for a reply and fold it back if `true`; otherwise publish fire-and-forget | `false` |
  | `timeout`    | how long a request waits for a reply (e.g. `30s`)             | queue service default |

- The **`queue` source** subscribes to a subject and runs each delivered message
  through its flow. For a message that came from a request it returns the flow's
  **final message as the reply** (correlated on the flow-event bus by `EventID`,
  exactly as the HTTP source does); for a fire-and-forget publish the queue layer
  simply drops the reply — one handler serves both. A handler holds a listener until
  its flow finishes, which bounds in-flight work to the listener count.

  | setting     | meaning                                                       | default |
  | ----------- | ------------------------------------------------------------- | ------- |
  | `subject`   | subject to subscribe to (required)                            | —       |
  | `listeners` | concurrent handler goroutines on this replica                 | `8`     |
  | `timeout`   | how long a handler waits for its flow to finish               | queue service default |

The `queue` connector has **no global config**, so there is nothing to declare:
a `source` (or `queue-dispatch`) of `type: queue` resolves it implicitly and the
runtime starts a default instance on demand.

```yaml
flows:
  - name: orders-api
    source: { connector: api, type: http, settings: { path: /orders/{id} } }
    process:
      - { type: queue-dispatch, settings: { subject: '"audit-work"' } }                    # publish (default)
      - { type: queue-dispatch, settings: { subject: '"enrich-work"', awaitReply: true } }  # request; reply folds back

  - name: order-enricher                 # competing consumer; scale replicas to load balance
    source:
      type: queue                        # implicit connector — no instance to declare
      settings: { subject: enrich-work, listeners: 8 }
    process:
      - { type: set-payload, settings: { value: '{"order": body, "status": "accepted"}' } }
```

See [`samples/queue-loadbalance.yaml`](../samples/queue-loadbalance.yaml) for a
fuller example (HTTP entry point, a request worker whose reply folds back, and a
one-way audit worker).

### Platform events: the `events` source and `publish-event` block

Topics are the **broadcast** counterpart of queues — also a core runtime service
(in-process in the standalone module, NATS-backed in the k8s module, under a
separate `octo.<id>.t.<subject>` scope). Where a queue delivers each message to
**one** competing consumer, a topic **fans every message out to every subscriber**.
Use it for pub/sub: one producer, many independent reactions.

- **`publish-event`** (a processor block) broadcasts the current message to a
  subject. Its `subject` is a CEL expression (per-message routing); an optional
  `value` CEL expression replaces the published body (default: the whole body). It
  is fire-and-forget and passes the message through unchanged.
- The **`events` source** subscribes to a subject and runs **its own copy** of each
  broadcast message through the flow. Every `events` source on a subject — across
  every replica — receives every message, so it does not load-balance. Its
  `subject` (required) and `listeners` settings mirror the queue source; there is no
  reply.

Like `queue`, the `events` connector has **no global config**, so a `source` (or
`publish-event`) of `type: events` resolves it implicitly.

```yaml
flows:
  - name: emitter
    source: { type: cron, settings: { schedule: "@every 3s", payload: '{"tick": string(now)}' } }
    process:
      - { type: publish-event, settings: { subject: '"notifications"' } }   # broadcast

  - name: subscriber-a                   # every subscriber on the subject gets every message
    source: { type: events, settings: { subject: notifications } }
    process:
      - { type: log, settings: { logger: out, message: '"A saw " + body.tick' } }
```

See [`samples/events.yaml`](../samples/events.yaml) for a runnable fan-out example
(one cron publisher, two subscribers that each receive every notification).

## Writing a connector source

A connector becomes a source by implementing `core.SourceProvider`. The runtime
hands the source the channel it should emit on; the source runs on its own
goroutine and must not send after `Stop` returns. See
[`runtime/connectors/noop/source.go`](../runtime/connectors/noop/source.go) for a
minimal reference implementation. A source reads its configuration from
`cfg.Settings` (a `types.Settings`) by decoding it into a typed struct.
