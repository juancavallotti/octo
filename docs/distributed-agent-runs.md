# Distributed Agent Runs

An `ai-agent` with a `memoryThreadId` claims that conversation for the length of
its run, so a later message on the same thread is handed to the run already
working on it rather than starting a second one. The claim is a map in the process
(`runRegistry`, `runtime/core/internal/engine/agentsignals.go`).

This page is about what that costs across replicas, what a distributed claim
would and would not fix, and what it would cost to build. It exists because the
answer is not "add a lock and it all works", and the difference is worth writing
down before anyone starts.

## Why the claim is process-local today

The handover has to be atomic. A caller that found a run, let go, and then
offered it a message could be handing work to a run that finished in between —
losing the message *and* stopping the caller's flow on the strength of having
placed it. One mutex around the lookup and the offer makes that impossible, and
a mutex is the whole implementation.

Anything cluster-wide has to reproduce that atomicity over a network, which is a
different and much larger problem. The http connector's registry of open SSE
streams (`runtime/connectors/http/sse.go`) is process-local for the same reason
and has lived with the same consequence.

## What breaks across replicas

Memory is not one of them, and that is worth saying first: transcripts live in
the deployment's object store, so a run that starts on another replica has the
whole conversation. Nothing is forgotten. What goes wrong is that two runs exist
where there should be one.

| # | Scenario | Consequence |
| --- | --- | --- |
| 1 | A follow-up lands on another replica | It starts its own run. Two answers on two streams, both billed. |
| 2 | A stop lands on another replica | It stops nothing **and reports success**. The run continues to completion, fully billed, while the caller believes it ended. |
| 3 | Two runs save one thread | `saveMemory` retries on a version conflict, but it retries by writing *its own* transcript — the retry does not merge. The later save wins entirely and the other run's turns are gone. |
| 4 | A replica dies mid-run | The claim dies with it. Memory is only written at the end, so the whole turn evaporates and the thread reverts to where it started. |
| 5 | A client reconnects to another replica | It cannot rejoin its own run. The run keeps streaming to a connection nobody is reading. |

(2) is the one to weigh most heavily. Everything else is visible — an extra
answer, a lost turn — and a user can tell something went wrong. A stop that
silently stops nothing looks exactly like success.

## What is broken on one replica too

These are not distribution problems. They are in the design as it stands, and a
distributed claim does not touch them.

| # | Scenario | Consequence |
| --- | --- | --- |
| 6 | A wedged run — a tool branch with no timeout — holds its id | Every follow-up is handed to something that will never answer. There is no maximum run age and no liveness check. |
| 7 | Follow-ups still pending when `maxIterations` is reached | They were accepted, and their senders' flows stopped, and then the loop falls to the guardrail without ever injecting them. Accepted and silently dropped. |
| 8 | `pending` is unbounded | A client sending follow-ups faster than the agent answers grows it without limit. |

A distributed claim would make (6) *worse*, not better: a wedged run would then
hold the conversation for the whole cluster rather than for one replica.

## What a distributed claim actually fixes

| | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **Session affinity** (routing) | fixed | fixed | fixed | — | fixed | — | — | — |
| **Lease claim + core NATS delivery** | fixed | fixed | fixed | partly | — | worse | — | — |

Two things fall out of that table.

**Session affinity is the cheapest complete answer to the cross-replica half.**
It costs nothing in the runtime — it is an ingress setting keyed on the same
thread `memoryThreadId` is — and it fixes (5), which a claim cannot, because
rejoining a stream is about where the *connection* lands, not about who owns the
conversation.

**A claim without event forwarding makes (5) worse.** Today a reconnecting
client at least gets a second run that answers it. With a cluster-wide claim it
gets told somebody else owns the conversation and receives nothing at all,
because the run's stream events are going to the replica it left. Solving that
properly means publishing the run's events cluster-wide and having any replica
able to relay them, which is a materially bigger feature than the claim.

**(4) is only partly fixed**, and only with a lease that expires. A claim with no
TTL held by a dead pod is worse than no claim at all. And even with one, a signal
published into the window between the death and the expiry is dropped — core
NATS delivers at most once, to whoever is subscribed at that moment.

## Which primitive

The deployment runs **core NATS with no JetStream and no volume**
(`helm/templates/nats/statefulset.yaml`), and stays that way. That looks at first
like it rules the approach out, because core NATS cannot express a claim:
subjects fan out to every subscriber, and a queue group distributes among them
rather than refusing a second member. There is no "subscribe and find out
somebody else has it".

**But the claim and the delivery are two jobs, and only one of them is hard.**

- **Claiming** needs exclusivity — exactly one owner per conversation, decided
  atomically, with a way to recover when the owner dies. Core NATS has nothing
  for this.
- **Delivering** needs a message to reach one known subscriber. Core NATS does
  this perfectly well: the owner subscribes to the conversation's subject when it
  acquires the claim, and anyone who fails to acquire publishes there.

So the shape is: get a lock from somewhere else, keep the transport where it is.

### The lock: a Kubernetes Lease

**This part is now built**: `core.Leases` (`runtime/core/lease.go`) is an
accessor on `core.RuntimeServices`, implemented in both modules.

The k8s services module already held a `coordinationv1` client and most of the
RBAC for it — `runtime/services/k8s/leaderelection.go` uses Leases too — so it
needed **no new infrastructure**, and no JetStream. The one chart change is the
`delete` verb, since a claim is released by deleting its object rather than by
letting it lapse. A fail-fast `Create` on a
per-conversation Lease is an exact claim: it succeeds or it conflicts, with no
ambiguity and no waiting. The lease duration handles a holder that dies, and the
owner renews while it runs.

What it costs is etcd. An object write when a run starts, a renewal per run per
period, and a delete when it ends, against an API server that is not sized for
per-conversation traffic. That is comfortable at tens of concurrent
conversations and uncomfortable at thousands — the ceiling is real, but it is a
ceiling rather than a wall, and it is reached long after the point where any of
this matters.

Note that `core.LeaderElection.Acquire` is **not** the primitive, even though it
is built on the same object. It starts a background campaign and
returns a handle whose `IsLeader` converges later, so a false reading means "not
yet" rather than "somebody else has it" — the exact distinction a claim needs.
Its 15-second lease and 2-second retry are sized for one long-lived election per
process, not one per conversation. What this needs is a second, fail-fast
operation beside it, not a different use of that one.

### The delivery: core NATS, unchanged

`core.Queues` already carries a message to a subject. The owner subscribes on
acquiring the lease and unsubscribes on releasing it; a replica that fails to
acquire publishes and stops its flow. No JetStream, no persistence, no volume.

Delivery is at most once, and here that is the correct semantic rather than a
compromise: if nobody is subscribed, there is no run to steer, and the publisher
should start one instead of queuing a message for a conversation that has ended.
The one lossy window is a signal published between an owner's death and its
lease expiring — bounded by the lease duration, and the same window in which the
run itself was already lost.

### JetStream, if the ceiling is ever reached

If per-conversation Leases become too much for the API server, JetStream KV
gives the same claim with `Create` failing on an existing key, a bucket TTL, and
no etcd involvement — at the cost of enabling JetStream and giving NATS the
volume it deliberately does not have today. A JetStream durable consumer would
go further and collapse the claim and the transport into one primitive, which is
the most elegant version of all of this; it is also the one that needs the most
new machinery, and it is not worth reaching for until the Lease is actually
hurting.

## The seam it goes behind

The claim's whole surface is three methods on `runRegistry`:

```go
offerOrClaim(id, text string, mine *agentRun) (handedOff bool)
stop(id string) bool
release(id string, mine *agentRun)
```

A cluster-wide implementation implements the same three, over `core.Leases` for
the claim and `core.Queues` for the delivery.

An earlier draft of this page called for an **optional side interface the caller
type-asserts** — the `core.LogShipper` shape — on the grounds that the standalone
module cannot have a distributed claim. That reasoning was about the wrong thing.
A fail-fast lease is not a distributed claim: exclusivity is only ever asked of
the processes that could compete for a name, and in one process a map under a
mutex is the complete and exact implementation rather than a stand-in for a real
one. So it is an accessor, like `Queues()` and `KV()` — there is no module that
lacks it, and nothing for a caller to type-assert. See the worked example in
[extension-points.md](extension-points.md).

Nothing in a flow changes either way. `memoryThreadId` and `stopWhen` mean the same
thing whichever implementation is underneath.

## If you only do one thing

Turn on session affinity. It closes (1), (2) and (5) at the router, costs no
code, and leaves the claim question to be answered on its merits rather than
under pressure.

Then fix (6) and (7), which bite on a single replica and which no amount of
distribution will help.
