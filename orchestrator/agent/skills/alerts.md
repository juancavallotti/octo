# Creating a platform alert

A **watch** is a standing question, asked on a schedule, that announces something
when its answer holds. Somebody asks you for one in a sentence — "tell me when
checkout starts failing" — and your job is to turn that into a definition that
fires when they would want to be woken and stays quiet the rest of the time.

The second half is the hard half. A watch that fires on every blip gets muted, and
a muted watch is worse than no watch, because everyone believes it is still
guarding something.

## The order to work in

1. **Find the app.** A watch scoped to nothing watches the whole installation.
   That is almost never what was asked for. `GET /traces/apps` lists what is
   reporting, with the deployment and integration ids you will scope by.
2. **Pick the source and metric** from the catalogue below. If nothing in it fits,
   say so rather than approximating — a watch on the wrong number is a watch that
   lies quietly.
3. **Preview it.** `POST /alerts/preview` with the whole definition. Nothing is
   stored, nobody is told, and you get back what each condition observed and
   whether it held. Do this *before* you save, every time.
4. **Save it** with `POST /alerts/watches`.
5. **Tell them what you built**, in the sentence they asked in: what it watches,
   what makes it fire, how long it has to hold, and who hears about it.

Previewing is not optional politeness. A definition that is syntactically fine can
still be one that would never have fired, or one that fires right now on ordinary
traffic, and the preview is the only thing that tells you which before it is
somebody's pager.

## What you can ask about

Three sources. The metric name means different things in each, which is why the
source is a separate field and not a prefix.

**`traces`** — one row per run, so these are questions about work the platform did.

| Metric | Aggregates | Notes |
| --- | --- | --- |
| `traces` | `count` | how much ran |
| `failed_traces` | `count` | how much of it failed |
| `error_rate` | `ratio` | `failed_traces / traces`, rendered as a percentage |
| `duration_ns` | `p95`, `avg`, `max` | nanoseconds |
| `cost_usd` | `sum` | model spend |
| `tokens`, `llm_calls`, `unpriced_calls` | `sum` | model accounting |

**`logs`** — `events` (`count`) and `error_rate` (`ratio`, error events over all
events). Scope may carry `levels` and a `search` substring.

**`pod_stats`** — CPU, memory and the runtime's own Prometheus metrics, per pod.
The names are open, because they come from whatever the runtime exports rather
than from a list this service keeps: ask `GET /stats/{deploymentId}/metrics` for
what a given deployment actually reports. Scope takes `across` to say how the
per-pod series collapse to one number, because a watch is about the deployment and
the pods underneath it come and go.

**One caveat worth knowing on pod stats.** A metric only appears there once it has
reported at least once, so a counter that has never been incremented cannot be
selected — `octo_flow_errors_total` most notably. For "this app is erroring", the
trace-based `error_rate` is both always present and the better question anyway.

## The three kinds of condition

**`threshold`** — "above this number". `{"op": "gt|gte|lt|lte", "threshold": N}`,
plus `windowBuckets` for how wide the comparison is and `minSamples` for how much
data it needs before it will judge at all. On a ratio, `minDenominator` is what
stops "one request, and it failed" from reading as a 100% error rate.

Reach for this when there is a number somebody can name. "Error rate above 5%" is
a threshold; "error rate is unusual" is not.

**`spike`** — "much worse than it has been". Compares a recent window against a
baseline, with a guard band held open between them so a slow ramp cannot quietly
become its own baseline. Three gates, all of which must clear: `z` (large relative
to how much this series normally moves), `minDelta` (large in absolute terms) and
`minRatio` (large in proportion).

`minDelta` is the one people leave out and then regret. Over a quiet enough
history the statistical gate will eventually call any change significant, and
`minDelta` is how you say "two more failures is not an incident however surprising
it is".

**`absence`** — "it stopped reporting". `forBuckets` is how long the silence has to
last; `baselineBuckets` and `minBaseline` are the evidence that it used to report,
which is what keeps this from firing for every app that has never run.

Absence and every downward threshold fire on *small* numbers, which a broken
ingest pipeline also produces. That is worth saying out loud when you build one.

## Scope, and why an unscoped watch is usually wrong

`deploymentId`, `integrationId`, `appName`, `appVersion` narrow what is counted.
A watch with none of them counts everything in the installation, so one noisy app
fires the watch that was meant to guard another.

A log `search` **must** carry a `deploymentId` or an `appName` — it is refused
otherwise, because a substring predicate with nothing to narrow it is a scan of
every log line in the installation, once a minute, forever.

## Timing

Four durations, all seconds on the wire.

- **`step_seconds`** — bucket width. 30s minimum, 1h maximum.
- **`interval_seconds`** — how often it is asked. Same bounds. Asking more often
  than the buckets close just re-reads a bucket already judged.
- **`for_seconds`** — how long the verdict must hold before it announces. This is
  the main defence against noise, and it is counted in consecutive firing
  evaluations rather than wall-clock: five minutes of firing with a gap in the
  middle is not five minutes of firing.
- **`cooldown_seconds`** — how long it stays quiet afterwards. It is the *only*
  bound on repeats: a still-firing watch offers to say so on every evaluation, and
  zero lets every one of them through.

Sensible defaults for a first watch: `step` 60, `interval` 60, `for` 300,
`cooldown` 1800. Tighten from there if they tell you it is too slow.

## Actions

`topic` publishes into a deployment's own subject, where a flow picks it up with an
ordinary `events` source — this is how an alert reaches the troubleshooter.
`{"deploymentId": "...", "subject": "...", "reportTo": ["..."]}`.

`email` sends to named addresses: `{"to": ["..."]}`.

A watch with no action still evaluates and still records incidents; it just tells
nobody. That is occasionally what someone wants — a watch that only shows up in
the incident list — but say so explicitly rather than shipping it silently.

## The rest of the shape

`severity` is `info`, `warning` or `critical`, and it is a closed set because it
orders the incident list. `combinator` is `all` or `any` over the conditions.
`on_no_data` is `ok` (absence does not satisfy it), `fire` (absence satisfies it)
or `keep` (absence is unknown, and the state machine does not move).

Limits: 10 conditions per watch, 10 actions, 200 watches per installation, and a
name of at most 200 characters that will be rendered into notification subjects.

## A worked example

"Tell me when checkout's error rate goes above 5% for five minutes."

```json
{
  "name": "checkout error rate",
  "description": "Above 5% of runs failing, held for five minutes.",
  "enabled": true,
  "severity": "critical",
  "combinator": "all",
  "conditions": [
    {
      "id": "error-rate",
      "type": "threshold",
      "source": "traces",
      "metric": "error_rate",
      "aggregate": "ratio",
      "scope": { "appName": "checkout" },
      "params": { "op": "gt", "threshold": 0.05, "windowBuckets": 5, "minDenominator": 20 }
    }
  ],
  "actions": [{ "id": "mail", "type": "email", "params": { "to": ["oncall@example.com"] } }],
  "on_no_data": "ok",
  "step_seconds": 60,
  "interval_seconds": 60,
  "for_seconds": 300,
  "cooldown_seconds": 1800
}
```

`minDenominator: 20` is the part that is not in the request and belongs anyway:
without it, the first failed run of a quiet morning is a 100% error rate and the
watch fires on a single request. Threshold `0.05` is a proportion and not a
percentage — 5 would mean 500%, and would never fire.

## What to tell them afterwards

Give them the name, what it watches, the number that makes it fire, how long it
has to hold, and who gets told. If you added a guard they did not ask for — a
`minDenominator`, a `for` window — say that you did and why, in one sentence. They
asked for a threshold; you also made a decision about noise, and it is theirs to
disagree with.

If the preview came back already firing, lead with that. Either it is a real
problem they did not know about, or the threshold is wrong, and both are worth
more than the confirmation that the watch was saved.
