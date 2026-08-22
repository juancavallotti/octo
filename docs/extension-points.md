# Extension Points

Where new runtime capability goes, and — more often the useful question — where it
does not. This page is the rule; the walkthroughs live in
[the extending section](../apps/docs/content/docs/extending/) of the published
docs.

## The one idea

**The runtime has exactly two extension points: `services` and `connectors`.**

New capability goes into one of them, or into the engine. It does not go into a
third registry. Someone asking "how is the runtime extended?" should find two
answers and be done, not four answers and a judgement call. A parallel seam
package is the failure mode this page exists to prevent: it is always locally
reasonable and always leaves the next person one more place to look.

```text
runtime/
  services/     <- extension point 1: what the runtime IS
    standalone/     provider   (core.RuntimeServices)
    k8s/            provider   (core.RuntimeServices)
    observability/  hosted     (probes, metrics)
  connectors/   <- extension point 2: what a flow can DO
    http/  cron/  database/  logger/  slack/  ...
  core/
    internal/engine/  <- not an extension point; see "The engine" below
```

Both are wired the same way: a subpackage, an `init` that registers, and a blank
import in `runtime/octo/main.go` deciding what the binary ships. Nothing is
discovered, nothing is configured by path.

### One `init()` per module

**Exactly one `func init()` per loadable module**, in the module's root file. That
`init()` is the module's *manifest*: it registers everything the module
contributes, in a written-down order, and it is the answer to "what does importing
this package put into the runtime?"

The registration itself stays where it belongs — beside the block it registers — as
an ordinary named function called from the manifest:

```go
// pinecone.go — the module's one entry point
func init() {
	registerConnector()
	registerQuery()
	registerUpsert()
	registerFetch()
	registerDelete()
}

// query.go — was an init(), now named and called from above
func registerQuery() {
	core.MustRegisterBlock("pinecone-query", newQuery)
	core.RegisterBlockMeta(core.BlockMeta{ /* unchanged */ })
}
```

The unit of loading is a package, so the unit of registration must be a package
too. One `init()` per file gives you the opposite: side effects that run in
filename order, and a new block that extends the runtime's surface without
touching any file a reviewer would think to open.

Two things to keep straight when writing one:

- **Stay in the module's own directory.** `RegisterBlockMeta` /
  `RegisterConnectorMeta` / `RegisterExtension` bind metadata to the caller's
  source directory (`callerDir()`, `runtime/core/schema_meta.go`). Naming a
  function is safe; moving a registration helper into a *different* package
  reattaches its palette group and icon defaults to that package's directory.
- **`gochecknoinits` enforces the rule.** It is enabled in
  `runtime/.golangci.yml` with an allowlist naming the one permitted file per
  module — that allowlist is the enumeration of loadable modules. A new module adds
  an entry; a second `init()` inside a module fails CI.

They are *activated* differently, which is the part worth keeping straight. A
build tag decides which packages are compiled in. Among the providers compiled in,
`RUNTIME_SERVICES_MODULE` then selects the one that is active; hosted services have
no such selection and every one compiled in runs.

## `services` — what the runtime is

A runtime service supplies the platform the flows run on. It has two facets, and a
package may implement either, or both.

### Provider facet

Supplies `core.RuntimeServices`: leader election, leases, KV, secrets, queues,
topics, resources. Providers are **module-selected** — exactly one is active, chosen by
`RUNTIME_SERVICES_MODULE` — so registration is conditional by design:

```go
func init() { services.Register(Module, New) }   // a no-op unless Module is selected
```

Every imported provider registers unconditionally; only the selected one wins. That
is why a binary can blank-import several without ambiguity, and why the k8s
provider's client-go and NATS dependencies stay out of the default build (the
blank import is behind `//go:build k8s` — see `runtime/octo/providers_*.go`).

**Adding a capability to `core.RuntimeServices` is usually the wrong move.** The
interface is already `//nolint:interfacebloat`, and widening it forces every
provider to implement something most of them cannot. The question to answer first
is whether the capability is universal or optional.

- **Optional — only some modules can have it.** Use an **optional side interface
  the caller type-asserts**. `core.LogShipper` is the worked example: the k8s
  module ships log records to a central subject, the standalone module ships
  nothing at all, so `LogSink()` may return nil and `teeDefaultLoggerToSink`
  asserts and nil-checks rather than assuming. This is the common case — reach
  for it first.
- **Universal — every module has one, they just differ.** Then it is an accessor,
  like `Queues()`, `KV()` and `Traces()`. The standalone module queues in process
  and the k8s module queues on NATS; the standalone module traces to a file and
  the k8s module traces to a subject. There is no nil to check and no assertion
  to write, because there is no module that lacks it. Forcing one of these
  through a side interface buys nothing and costs every caller a type assertion
  that can never fail.

`Leases()` is the case worth studying, because it is the one that looks optional
and is not. A *distributed* claim is something the standalone module genuinely
cannot have, and reading it that way argues for a side interface. But a
**fail-fast lease** is not a distributed claim: exclusivity is only ever asked of
the processes that could compete for a name, and in a single process a map under
a mutex is the complete and exact answer rather than a degraded stand-in — the
same relationship an in-process channel has to a NATS queue group. The test is
about the capability as the caller asks for it, not about the machinery
underneath.

The test is not "is this new", it is "**could a module reasonably not have it?**"
If yes, side interface. If no, accessor.

### Hosted facet

Runs for the life of the process rather than supplying capabilities: it starts
before the first flow generation, stops after the last, and spans every `--watch`
reload. It has no YAML — a hosted service configures itself from **CLI flags**,
which is why `HostedService` registers flags rather than parsing settings.

```go
func init() { services.RegisterHosted(New()) }   // never a no-op
```

Hosted services are not module-selected: every one compiled in runs. A binary
chooses which it ships purely by blank import.

Three obligations that are easy to miss:

- **`Flags` runs before the parse.** Resolve environment defaults there — a flag
  whose default is the env value gives "flag wins when passed" for free and prints
  the effective value in `--help`. Note that `.env` files are **not** loaded yet;
  dotenv resolution happens during config load, after the parse.
- **`Usage` is not optional if you have flags.** `usageText()` appends it to
  `octo --help`. A flag that exists but is absent from the help page may as well
  not exist.
- **`Start` must not block, and its errors are fatal.** Bind the port there, so a
  conflict fails the run instead of becoming a log line nobody reads.

`ConfigAware` is the optional half, called once per config generation — again on
every reload. A generation is not a fresh start: counters and other accumulated
state must survive it. Treat the call as "the config is now X", not "begin".

## `connectors` — what a flow can do

A connector owns a resource (a server, a pool, a client) and the things a flow
reaches it through. The registration is not one call — the parts that ride along
are the ones people forget:

| Register | For | Where |
| --- | --- | --- |
| `core.MustRegisterConnector` | the connector itself | `runtime/core/registry.go` |
| `core.MustRegisterBlock` | each block it contributes | `runtime/core/block_registry.go` |
| `core.SourceProvider` | sources, as an optional interface on the connector | `runtime/core/source.go` |
| `core.RegisterConnectorMeta` / `RegisterBlockMeta` | the editor schema | `runtime/core/schema_meta.go` |
| `core.RegisterExtension` | package-level palette defaults (group, icon) | `runtime/core/schema_meta.go` |

`runtime/connectors/logger` is the smallest complete example: `logger.go` holds the
module's one `init`, calling `registerConnector` (the connector, an extension
default, and connector meta) and `registerLog` (the block, over in `log.go`).
`runtime/connectors/pinecone` is the same shape at five blocks.

Sources are **not** a separate registry. A connector implements `SourceProvider`
and builds them, so a source closes over the connector's own connections instead
of reaching for a global. Some connectors are resolved implicitly by source type
and are never declared in YAML at all (queue, cron) — do not add a settings-less
connector declaration to make one appear.

Two rules that CI enforces:

- **The editor schema is generated from Go.** `octo schema` walks the registered
  meta and the `octo:` struct tags. A block whose settings struct has no tags is a
  block the editor cannot render.
- **An undocumented registered type fails the build.** Every connector, block and
  source must be named in exactly one `apps/docs/content/docs/reference/connectors/*.mdx`
  page's `octo_types` frontmatter. `scripts/check-docs-drift.mjs` compares the two
  and fails on drift (`task docs:check`, and the `validate` workflow).

## The event seams are not an extension point

`core.EventBus` (per-message flow events) and `core.BlockEvents` (pre/post-invoke
per block) let you **observe** the runtime without registering anything into it.
Subscribing is not extending: nothing you attach changes what a flow does, and
there is no name to collide with.

Reach for them before either extension point when the requirement is "know what
happened". The observability service is entirely built on them — it registers no
connector and no block, and touches no engine internals.

The contract, in short: flow-event handlers and block listeners all run inline on
the flow's own goroutine and must return fast. Block listeners name the paths they
want (`AddSyncFor`) so a block nobody asked about is never built into an event,
and delivery is exactly once — there is no queue and nothing is dropped. A
listener may read and copy `BlockEvent.Message` while it runs, but must not retain
the pointer. The full version, with the cost analysis, is in
[processing-pipeline.md](processing-pipeline.md) and
[the monitoring page](../apps/docs/content/docs/runtime/monitoring.mdx).

## What is deliberately not an extension point

**The engine.** Composite blocks — anything with an inline sub-flow slot (`fork`,
`foreach`, `if`, `cache-scope`, `ai-router`, `ai-agent`, `ai-retry`) — live in
`runtime/core/internal/engine` and cannot live anywhere else: building a sub-flow
needs the builder, which is internal. A block that wants to run a nested chain is
an engine change, not a connector.

The engine also owns the **leaf** blocks that belong to no connector: `ai-mapping`
and `ai-embed` bind to whatever LLM connector a flow names, through the shared
`core.LLMClient` / `core.EmbedClient` interfaces, so they are provider-agnostic
runtime capability rather than part of any one provider's package. The test is
ownership, not shape: a leaf block goes in a connector package when it reaches a
resource *that package owns*, and in the engine when it works against an interface
any provider can satisfy. `connectors/llm/` holds LLM provider connectors only.

**CLI subcommands.** `run`, `invoke`, `eval`, `schema` are a closed switch in
`runtime/octo/main.go`. A hosted service extends the CLI by adding *flags to
`run`*, which is the seam that exists; there is no registry of subcommands and
adding one would be the third extension point this page is about.

**CEL functions.** Not a new registry either — they go through the single seam,
`expr.RegisterMessageExtension`, and all message CEL compiles via
`expr.CompileMessage`. Never compile at a call site.

## Choosing, in one pass

```text
Does a flow author reference it by name in YAML?
├─ yes → connector (+ its blocks, sources, meta, and docs page)
└─ no
   ├─ Does it supply platform capability the runtime already names
   │  (KV, queues, leases, leader election, …)?      → services: provider facet
   ├─ Does it run for the life of the process with
   │  its own flags?                                  → services: hosted facet
   ├─ Does it only need to watch what flows do?       → EventBus / BlockEvents
   ├─ Does it need to run a nested sub-flow?          → the engine
   └─ Is it a leaf block that owns no resource, binding
      to any provider through a shared interface?     → the engine
```

If the answer is "none of these", the answer is still not a new registry. Say so
in review and pick the closest of the five.
