# Processing Pipeline

This document describes the runtime building blocks that turn connector events
into messages and process them concurrently. It covers the conceptual model, the
configuration schema, the concurrency model, and the start/stop lifecycle.

> **Status.** The *structure* below is stable. The *execution model* of composite
> blocks (how `scope`/`fork`/future `loop` actually run, and what a multi-output
> processor returns) is intentionally deferred to a later iteration. Composite
> blocks are wired structurally today with provisional run semantics.

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
  and optional `alternative` (the catch / recovery / compensation flow).
- **`fork`** — a scatter / multi-branch block. Slot: `branches` (an array of
  flows).

The flow builder **dispatches on block type**: composite kinds build their typed
sub-flows directly; every other (leaf) block type is resolved through the
`core.BlockRegistry`. Leaf blocks self-register via `core.MustRegisterBlock` in
an `init` function, the same pattern connectors use.

> Adding a new composite *kind* (e.g. `loop`) means extending the builder and the
> config, not just registering a factory. This is the accepted cost of explicit
> typed slots while the set of composite kinds is small.

## Concurrency model

Each top-level flow is run by a **dedicated pool of worker goroutines** all
reading the same channel the source emits on. A worker takes a message and runs
it through the root flow's block chain.

- **No cross-flow ordering.** With more than one worker, messages may complete out
  of order. Set `workers: 1` for FIFO processing within a flow.
- **Backpressure** comes from the bounded source channel (`buffer`): when workers
  fall behind, the channel fills and the source blocks.
- A failing message aborts only that message — the worker survives poison
  messages and keeps processing.

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

Flows live under the top-level `flows:` key. A root flow binds a `source` and a
worker-pool size; sub-flows nested inside composite blocks must not declare
`source`, `workers`, or `buffer`.

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
    workers: 8          # pool size; defaults to 1 (FIFO)
    buffer: 128         # source -> pool channel depth; defaults to 64
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
