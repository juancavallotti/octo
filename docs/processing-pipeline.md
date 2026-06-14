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

## Writing a connector source

A connector becomes a source by implementing `core.SourceProvider`. The runtime
hands the source the channel it should emit on; the source runs on its own
goroutine and must not send after `Stop` returns. See
[`runtime/connectors/noop/source.go`](../runtime/connectors/noop/source.go) for a
minimal reference implementation.
