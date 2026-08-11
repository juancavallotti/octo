# Testing a flow

Tests live beside the flows they exercise, named for them: `orders.yaml` is tested by
`orders_test.yaml`. The loader skips `_test.yaml`, so a suite sits next to its flows
without ever being mistaken for one. The runner is `dolphin`.

## The suite

```yaml
flow: submit-order        # required — the flow every case calls
timeout: 30s              # a Go duration bounding any one case
env:                      # variables every case runs with
  API_BASE_URL: http://stub
inputs:                   # named messages the cases share
  small:
    data: { amount: 10 }
  a vip order:
    data: { orderId: 42, amount: 250 }
    vars: { x-api-key: dev-key }
mocks:                    # stand-ins for EVERY case, keyed by block address
  submit-order.charge:
    body: { status: "ok" }
cases:                    # required, at least one
  - name: a small order is accepted
    input: small
    expect:
      body: { status: "accepted" }
```

A file-level `mocks:` is where "no test in this file ever reaches the payment API" is
said once.

## Sources do not run

Under `invoke` nothing populates the message for you: no cron tick, no HTTP request,
no queue delivery. `vars` on an input is how a case seeds what a source normally
would — a flow reading `vars["x-api-key"]` needs it supplied there.

## expect

Omitting `expect` still asserts something: that the flow completed and did not fail.

| Field | Meaning |
| --- | --- |
| `body` | Compared **exactly**. A field in the result and not here is a failure. |
| `vars` | A **subset**: each key listed must match; anything else is ignored. |
| `that` | CEL expressions over the result, all of which must be true. |
| `dropped` | The flow filtered the message out. |
| `error` | The flow failed with a message **containing** this text. |

`dropped` and `error` are exclusive with each other and with the message fields — a
flow that dropped or failed returned no message to check.

`body` is exact because the body is the flow's answer, and a test earns its keep by
failing when something unexpected appears in it. `vars` is a subset because variables
are scratch space the engine adds to. Use `that: ['vars.size() == 2']` when you do
want it exact.

The `error` match is a substring on purpose: a case expecting `card declined` that
passed on any failure would prove the wrong thing.

`env` is **not** bound inside an assertion — dolphin never loads your config. Assert
on what the flow put in the body or the variables.

## Cases

| Field | Meaning |
| --- | --- |
| `name` | What the case proves, in words. Must be unique in the file. |
| `input` | The name of an entry in `inputs`, or an inline input. |
| `mocks` | Replaces the file's mock for that address whole, or `null` to lift it and run the real block. |
| `env` | Overrides the file's, per variable. |
| `expect` | Above. |
| `spies` | What a watched block should have seen. |
| `timeout` | Overrides the file's. |
| `skip` | A reason. Reported as skipped; the flow is not called. |

## spies

```yaml
spies:
  demo.each-order[body].classify-order:
    count: 2
    records:
      - input: { that: ['vars.order.id == 1'] }
      - input: { that: ['vars.order.id == 2'] }
```

A block inside a `fork` branch or `foreach` body is crossed once per branch or item,
so the count **is** the iteration count. `count: 0` is an assertion — it says the
block never ran, which is how a case proves a branch was not taken. Records are
positional; asserting on fewer crossings than happened is fine, naming more is a
failure.

## Writing tests that are worth having

A test that passes whether or not the code works is worse than none, because it reads
as coverage. Before writing an assertion, ask what change would break it — if the
answer is "none", the assertion is decoration.

Prefer proving the thing the flow exists for over proving it ran. `count: 0` on an
error path, an exact `body`, and an `error` substring that names the actual failure
all fail for a reason. `expect: {}` on a happy path asserts only that nothing blew
up, which is sometimes exactly what you want and should be said deliberately.
