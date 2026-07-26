# Extension Points

Where new runtime capability goes, and — more often the useful question — where it
does not. This page is the rule; the published docs carry the walkthroughs.

## The one idea

**The runtime has exactly two extension points: `services` and `connectors`.**

New capability goes into one of them, or into the engine. It does not go into a
third registry. Someone asking "how is the runtime extended?" should find two
answers and be done, not four answers and a judgement call. A parallel seam
package is the failure mode this page exists to prevent: it is always locally
reasonable and always leaves the next person one more place to look.

```
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

Both work the same way: a subpackage, an `init` that registers, and a blank import
in `runtime/octo/main.go` deciding what the binary ships. Nothing is discovered,
nothing is configured by path.

## `services` — what the runtime is

A runtime service supplies the platform the flows run on. It has two facets, and a
package may implement either, or both.

### Provider facet

Supplies `core.RuntimeServices`: leader election, KV, secrets, queues, topics,
resources. Providers are **module-selected** — exactly one is active, chosen by
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
provider to implement something most of them cannot. The pattern instead is an
**optional side interface the caller type-asserts** — `core.LogShipper` is the
worked example: the k8s module implements it, the standalone module does not, and
`teeDefaultLoggerToSink` asserts and nil-checks rather than assuming. Copy that
shape before you copy the interface.

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

`runtime/connectors/logger` is the smallest complete example: one `init` doing a
connector registration, an extension default, and connector meta, with the block
registered alongside.

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

The contract, in short: flow-event handlers and **sync** block listeners run
inline on the flow's own goroutine and must return fast; **async** block listeners
run on the dispatcher's goroutine, are at-most-once (a full queue drops and
counts), and must not dereference `BlockEvent.Message.Body` or `.Variables`. The
full version, with the cost analysis, is in
[processing-pipeline.md](processing-pipeline.md) and
[the monitoring page](../apps/docs/content/docs/runtime/monitoring.mdx).

## What is deliberately not an extension point

**The engine.** Composite blocks — anything with an inline sub-flow slot (`fork`,
`foreach`, `if`, `cache-scope`, the `ai-*` family) — live in
`runtime/core/internal/engine` and cannot live anywhere else: building a sub-flow
needs the builder, which is internal. Connector packages register **leaf** blocks
only. A block that wants to run a nested chain is an engine change, not a
connector.

**CLI subcommands.** `run`, `invoke`, `eval`, `schema` are a closed switch in
`runtime/octo/main.go`. A hosted service extends the CLI by adding *flags to
`run`*, which is the seam that exists; there is no registry of subcommands and
adding one would be the third extension point this page is about.

**CEL functions.** Not a new registry either — they go through the single seam,
`expr.RegisterMessageExtension`, and all message CEL compiles via
`expr.CompileMessage`. Never compile at a call site.

## Choosing, in one pass

```
Does a flow author reference it by name in YAML?
├─ yes → connector (+ its blocks, sources, meta, and docs page)
└─ no
   ├─ Does it supply platform capability the runtime already names
   │  (KV, queues, leader election, resources)?      → services: provider facet
   ├─ Does it run for the life of the process with
   │  its own flags?                                  → services: hosted facet
   ├─ Does it only need to watch what flows do?       → EventBus / BlockEvents
   └─ Does it need to run a nested sub-flow?          → the engine
```

If the answer is "none of these", the answer is still not a new registry. Say so
in review and pick the closest of the five.
