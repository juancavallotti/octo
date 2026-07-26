# The Debug Seam

How `--break-at`, `--spies` and `--mocks` are built, and what to keep in mind if
you add a fourth. User-facing docs live in
[the debugging guide](../apps/docs/content/docs/guides/debugging-flows.mdx); this
page is about the machinery.

## The one idea

**A debug feature is not a check in the execution loop. It is a rewrite of the
config before the flow is built.**

The engine's `Flow.Process` knows nothing about breakpoints, spies or mocks. It
runs blocks in order, exactly as it always did. What changes is the tree it was
built from: the runtime resolves the address you typed to a `types.BlockConfig`,
and swaps that slot for a block of its own.

```
--spies 'orders.charge'

  before                      after
  ─────────                   ─────────────────────
  { type: rest,               { type: spy,
    name: charge }              name: charge,          <- the target's label
                                settings: {address},
                                process: [
                                  { type: rest,
                                    name: charge }     <- the untouched original
                                ] }
```

The engine then builds that like any other composite. The result is that a debug
feature costs the hot path nothing, and the code that implements one is a block
like any other.

Everything below follows from this.

## The four pieces

Adding a debug feature means touching four places. Follow `spy` — it is the most
complete example.

| Piece | Where | What it does |
| --- | --- | --- |
| **Collector** | `core/spy.go` | Holds what was observed. On `core.BlockDeps`, so every block can reach it. |
| **Engine block** | `core/internal/engine/spy.go` | The block that gets injected. Wraps or replaces the target. |
| **Injector** | `core/runtime/spy.go` | Resolves the address and rewrites the config. |
| **Service option** | `core/runtime/runtime.go` | `WithSpies(...)`, and a line in `applyDebug`. |

Plus the CLI flag (`cli/debug.go`) that builds the collector from user input.

### Addressing

All three share one resolver: `resolveTarget(cfg, kind, addr)` in
`core/runtime/address.go`. It parses the grammar
(`<flow>[<chain>].<block>[<branch>].<block>…`), walks down the composites, and
returns a **pointer into the config** — so the caller can rewrite that slot in
place.

The `kind` argument is there so a bad address blames the flag the user typed
(`spy "orders.nope": no block …`), not whichever feature happens to own the
resolver.

### Collectors are out-of-band, and they have to be

A spy's records do not travel back in the message. They go into a collector
hanging off `BlockDeps`. That is not laziness — it is forced:

> **A `fork` runs each branch on a `msg.Clone()` and throws the branch's message
> away.** An `enrich` body does the same. So a block inside one has *no way* to
> report anything through the flow's result.

Any observation from inside a composite must leave the flow sideways. If you add
a feature that observes, its results go in a collector.

## The rules you can't see from one file

These are the constraints that only show up when the features interact. They are
each pinned by a test; the test names are given so you can find the reasoning.

### 1. Injection order is `mocks → spies → breakpoint`

Producing `breakpoint[ spy[ mock ] ]`. Both ends are forced:

- **A mock must be innermost** because it *replaces* its target. Inject it after a
  spy and it would replace the spy, and the spy would be gone.
- **A breakpoint must be outermost** because it halts the flow by calling
  `RequestStop()`, which writes a reserved variable into the live message. A spy
  wrapped *around* it would record that bookkeeping as part of the block's
  output — the exact leak `TestBreakpointSnapshotOmitsStopFlag` exists to prevent.

See `applyDebug` and `TestApplyDebugNestsMockInsideSpyInsideBreakpoint`.

### 2. A wrapper is transparent to a later address

Once `orders.fanout` is spied, that slot holds a `spy`, and a spy has no branches
— so a *second* address descending into `orders.fanout[audit].log-it` would fail
to resolve.

`unwrapDebug` peels debug wrappers **on the way down, never at the target**. The
asymmetry matters: an address landing *on* an already-wrapped block must return
the wrapper's slot, so the next injection nests *around* what is there rather
than replacing it. (`TestInjectSpiesDescendsThroughAWrappedComposite`.)

The flow builder mints the same addresses on the way down, for block events, and
carries the mirror image of this rule: `builder.block` gives a spy or breakpoint
wrapper no path of its own and builds its inner chain at the slot the wrapper
sits in, rather than at a branch of it. Without that, a spied `orders.charge`
would report as `orders.charge[process].charge` — a path that does not resolve,
because the resolver peels the spy and then asks the leaf target for a `process`
branch it does not have. A **mock** needs no such case: it stands in its target's
place rather than wrapping it, so its natural path is already the address the
user gave, and it reports under it. (`TestBlockPathSpyWrapperIsTransparent`,
`TestBlockPathMockKeepsTargetAddress`.)

### 3. A wrapper must not change the program it observes

A block error is labelled with the block's name, and an error path can branch on
`vars.error.block`. So a wrapper:

- takes the **target's label** (`blockLabel(target)`), and
- calls `unlabel(err)` on the way out, to strip the label its own sub-flow added.

Without both, a wrapped block reports as `block "x": block "x": …` and the
wrapper shows through. `TestSpyPreservesErrorLabel` pins the wrapped error against
the unwrapped one, for a leaf *and* a composite — the composite case also catches
stripping one layer too many.

The same rule is why a mock takes its target's label: a mocked failure must be
indistinguishable from the real block failing.

### 4. Wrap, don't append

A spy wraps its target in a one-block sub-flow rather than sitting *after* it in
the chain. That is what lets it see the two outcomes that are **not** a message —
a block that drops (`nil, nil`) and a block that fails — because both are visible
on the way out of the sub-flow, where a following block would simply never run.

### 5. Records are clones, taken on both sides

Blocks mutate the message **in place** and return the same pointer. A record taken
after the block would show the input already overwritten by the output. So the spy
clones the input *before* running the block, and clones the output *after*.

### 6. One processor instance serves every worker

A flow's blocks are shared across its worker pool. Anything a debug block holds
must be safe for concurrent use, and anything it hands to a message must not be
shared *between* messages — which is why the mock encodes its canned body once at
build time and decodes a **fresh copy per message**. Handing out one decoded map
would let a downstream block mutating one message's body rewrite the mock's answer
for the next. (`TestMockBodyIsNotSharedBetweenMessages`.)

### 7. Debug blocks are unauthorable, and stay out of the schema

Each builder refuses to build when its collector is absent
(`errSpyUnwired`, `errMockUnwired`, `errBreakpointUnwired`), so a config that
declares `type: spy` by hand fails loudly.

They are also **deliberately absent from `RegisterBlockMeta`**. That keeps them out
of the editor palette and out of `octo schema` — and it is what keeps
`scripts/check-docs-drift.mjs` quiet, since that check requires every
schema-visible type to be documented on a reference page. If you register a debug
block's metadata, CI will tell you about it.

### 8. Invoke-mode only

`applyDebug` refuses all three outside invoke mode. It is **one guard over three
features that fail for different reasons**, and the distinction matters if you ever
want to relax it (see
[#150](https://github.com/juancavallotti/octo/issues/150)):

- A **breakpoint** would halt whichever production message happened to arrive
  first. This one should stay refused.
- A **mock** would answer live traffic with a canned response. Dangerous by
  default — but a mock cannot be authored in a config (the block is unregistered
  and refuses to build unwired), so it can only arrive as an explicit `--mocks`
  flag. Nothing else stands in its way.
- A **spy** is read-only, and yet it is the one with a real engineering problem:
  nothing *drains* its collector outside an invoke, so on a source-backed flow it
  would grow without bound, hoarding a clone of the input and output of every
  message that ever crossed the block, for nobody. Allowing it needs a bounded
  buffer, or a way for records to leave the process.

If you add an **observing** feature, it inherits the spy's problem. If you add one
that only *changes* what a flow does, it inherits the mock's.

## What block events do and do not replace

`core.BlockEvents` (see [processing-pipeline.md](processing-pipeline.md#block-events))
brackets **every** block with a pre- and post-invoke event carrying that block's
address — which overlaps what a spy does, and is not subject to rule 8, because it
retains nothing: a listener that keeps records is the listener's problem, not the
runtime's.

The spy is still its own feature, and the overlap is not yet worth collapsing:

- `--spies` **validates its addresses against the config at build time**, so a
  typo is an error naming the flag. A path filter over events would silently match
  nothing.
- A spy record is a **clone taken on both sides** (rule 5). A listener would have
  to clone in a sync callback, which puts rule 8's unbounded-growth problem right
  back where it was.
- Wrapping is what makes `unlabel` and the injection order (rules 1 and 3) mean
  what they mean; a listener does not wrap, so error labelling would change.

Reimplementing spies on the event seam is a change of its own, with those three
things to answer for.

## Conflicts

A mock deletes the subtree it replaces. Anything addressed *inside* it can never
fire — and would otherwise fail with `no block "log-it" in that chain`, which
reads like a typo when the truth is that the user mocked away the thing they were
trying to watch.

`checkMockConflicts` runs **before any injection**, while every address still
resolves, so the error can name both sides. Its structural check is careful about
one thing: the same block name reached through a *different* branch is a different
block (`TestIsInsideDistinguishesPaths`).

Any future feature that removes blocks needs the same check.

## The output envelope

A run that observes nothing prints its result **message** — `cli/invoke.go`'s
`printFlowResult`, the same `{event_id, variables, body}` a breakpoint reports.
Both paths mean the message by "the result", so a run that finishes never says less
than the same run stopped at its last block. Whatever a caller sees, it sees a
message; only the wrapper differs.

Both print it through `types.Message.Reported()`, which drops the engine's internal
stop flag from `variables`. A filter block sets it to end the flow, so it rides in
the message a filtered flow returns — it is bookkeeping between the engine and its
blocks, not a variable the flow set, and reporting it as one would be a lie.

`cli/debug.go` prints one JSON object for a run that *was* asked to observe itself.
One field is load-bearing beyond its appearance:

**`reached` is a `*bool`.** `packages/run-host/src/session.ts` identifies a break
envelope by sniffing for a *boolean* `reached`. So it must marshal as `false` for a
breakpoint that was never hit, and be **absent** when there was no breakpoint at
all — otherwise a spies-only envelope would be read as a breakpoint that never
fired. A plain `bool` cannot express both. `TestBreakEnvelopeStaysCompatible` and
`TestSpiesOnlyEnvelopeOmitsReached` pin the two directions.

If you grow the envelope, grow it **additively** and keep that sniff working.

## The seam has four consumers, not one

The CLI was the first, and for a while the only one. It is now the *bottom* of a
stack, and a feature that stops there is only a quarter built:

```
  editor canvas ─┐                         a block, clicked
                 ├─► @octo/editor ──┐
  MCP tool ──────┘                  ├─► @octo/run-host ─► octo invoke ─► the seam
                                    │      (spawns it)      (flags)
  a human at a terminal ────────────┘

  a _test.yaml ─────► dolphin ──────────► octo invoke ─► the seam
                    (spawns it)         (--run-debug-config)
```

**`packages/run-host`** (`session.ts`) is the one place that spawns the runner, so
every consumer above it reaches the seam through the same argv. It also decodes
the envelope — and normalizes it, which is the part to be careful with: under
`--spies` stdout carries the envelope *instead of* the result message, so
`InvokeResult.output` is re-derived from `envelope.result`. Otherwise switching on
a spy — which is meant to be read-only — would change what every existing reader
of `output` sees.

**The editor** (`packages/editor/src/app/run/`) has the harder problem, and it is
worth understanding before touching it:

> **A breakpoint dies with the run. A mock does not.**

`planBreakpoint` may invent synthetic names (`__bp_1`) for ambiguous blocks,
because it does so in a *clone* that is serialized, run, and thrown away. A mock
or a spy is saved to `.octo/editor-meta.json` and has to be found again on the next
reload — when every client id is new. So it is keyed by `naturalAddress`, which
invents nothing and returns null for an ambiguous block; placing a mock on one
*names it for real* first (`namesNeededFor`). Both live in `run/address.ts`.

Anything you add that a user can **save** inherits this. Anything they merely
**run** does not.

**MCP** (`packages/mcp/src/tools/run.ts`) declares mocks structurally in its zod
schema rather than as a JSON blob, so the tool schema teaches an agent the shape.
The rules the schema *cannot* express — one outcome per case, an unmatched message
fails the block — go in the description, because the runtime enforces them and an
agent has nowhere else to learn them.

**dolphin** (`runtime/dolphin/`) is the one consumer that does not go through
run-host: it spawns `octo invoke --run-debug-config` itself, because a test case
*is* a debug config plus assertions. Two consequences follow from the seam being a
config rewrite rather than a check in the execution loop, and both are structural:

> **One case is one process.** Mocks and spies are baked into the flow tree at
> *build* time, so a service can only ever serve the case it was built for. There is
> no batching this.

> **A mock removes the block's subtree.** So a suite cannot assert on a decision made
> *inside* a mocked composite — mock an `ai-router` and its routes go with it. The
> way around it is to mock a leaf *inside* the composite, which leaves the composite
> itself running for real.

It reads the outcome from `--envelope-out <file>`, not stdout: a flow with a `log`
block writes to stdout too, and an envelope interleaved with a log line is not
JSON. **stdout is not the CLI's private channel** — anything that has to be parsed
needs a channel of its own.

## Adding a feature: the checklist

The runtime:

1. Collector in `core/`, field on `core.BlockDeps`.
2. Engine block in `core/internal/engine/`, registered in `compositeBuilders()`.
   Refuse to build without the collector. Do **not** add it to `RegisterBlockMeta`.
3. Injector in `core/runtime/`, using `resolveTarget(cfg, kind, addr)`.
4. `With…` option, a line in `applyDebug` **in the right order**, and the
   invoke-mode guard.
5. If it wraps: take the target's label, `unlabel` on the way out, clone what you
   record. If it removes blocks: add a conflict check.
6. Flag in `cli/debug.go`, a section in the
   [debug config](../runtime/octo/debugconfig.go), and a line in its
   [schema](../runtime/octo/debugconfig.schema.json) — the drift test will fail
   until you do.

Then, for each consumer:

7. **run-host**: the flag on the argv in `invoke()`, and the field on
   `InvokeResult`. If it observes, grow `DebugOutcome` **additively** and keep the
   envelope sniff working (see above).
8. **MCP**: the parameter on `invoke_flow`, structurally. State in the description
   whatever the schema cannot.
9. **Editor**: `FlowRunRequest`/`FlowRunOutcome` in `run/transport.ts`, both apps'
   `runInvoke` actions, and `FlowRunContext`. If it is something the user *saves*,
   it needs a durable address (`naturalAddress`) and a place in `meta/types.ts` —
   and `meta/rename.ts` has to re-root it when a flow is renamed, because an
   address begins with the flow's name.
10. **dolphin**: the section on the test file (`runtime/dolphin/internal/suite/`),
    its [schema](../runtime/dolphin/testconfig.schema.json) — drift-tested, so it
    fails until you do — and the assertion for it in `internal/assert/`. A feature
    that observes needs somewhere to say what it should have observed.
11. Docs: the [CLI guide](../apps/docs/content/docs/guides/debugging-flows.mdx), the
    [testing guide](../apps/docs/content/docs/guides/testing-flows.mdx), the
    [CLI reference](../apps/docs/content/docs/reference/cli.mdx), the
    [editor guide](../apps/docs/content/docs/editor/debugging-flows.mdx), and the
    [MCP reference](../apps/docs/content/docs/ai/platform-mcp.mdx).
