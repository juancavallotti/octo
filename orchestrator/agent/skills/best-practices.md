# Writing a flow well

A flow can be written many ways that all run. These rules pick one, so what you
write reads like the samples and tests cleanly. Load this before you write a
definition, and check your draft against it before you validate. Each rule has an
id; the docs page "Flow Best Practices" and the platform's MCP prompt use the same
ones.

## multi-transform — one block for a run of setters

Two or more `set-variable` / `set-payload` blocks in a row are one
`multi-transform`. Steps run in order and each sees the one before it:

```yaml
- type: multi-transform
  name: price-order
  settings:
    transforms:
      - setVar: orderId
        value: body.id
      - setVar: total
        value: body.qty * body.price
      - setBody: '{"orderId": vars.orderId, "total": vars.total}'
```

Each step has exactly one of `setBody` or `setVar` (+ `value`). A `set-payload`
with `rawBody` stays its own block: `setBody` has no raw mode. The checker fails
a chain of setters; a deliberate one carries `# practices: allow multi-transform`
on the line above.

## enrich-context — keep the body around a destructive call

A connector block's result replaces the body. When later steps still need the old
body, run the call inside `enrich` and lift out only what you need. Never save the
body in a var and rebuild it afterwards:

```yaml
- type: enrich
  name: find-reason
  setVars:
    reasonIds: body.map(r, r.id)     # evaluated against the scope's result
  body:
    process:
      - type: odoo-search-read
        name: search-reason
        settings: { connector: odoo, model: '"crm.lost.reason"', domain: '[["name", "=ilike", body.reason]]', fields: [id] }
```

`enrich` takes `body` (the scope), `setVars` and `setBody` at the block's top
level, not under `settings`. Omit `setBody` and the original body is kept.

## result-var — prefer resultVar for one call

If the destructive step is ONE connector operation with a `resultVar` setting
(odoo, mongodb, notion, pinecone, tavily, parallel, file read/write, slack lookup,
ai-embed), set `resultVar` instead of wrapping it: the result goes to
`vars.<name>`, the body is untouched. Use `enrich` for several steps, for deriving
a value, or for blocks without it: `flow-ref`, `rest`, `sql` have none. Check
`blocks` for whether a block has it; do not guess.

## subflows — split long flows

Past roughly 8–10 steps, or with a piece another flow could use, move the piece
into a sourceless flow (no `source:`) and call it with `flow-ref`. Each piece gets
its own suite. A two-way `flow-ref` REPLACES the caller's body with the subflow's
result and merges its vars back: wrap it in `enrich` when the caller needs its own
body afterwards; as the last step it needs nothing. The subflow sees the caller's
vars.

## globals-file — octo.yaml holds what flows share

In a multi-file project, `octo.yaml` holds `service`, `env`, `resources`, shared
connectors and `processors`; flows live in sibling files named for their area
(`contacts.yaml`). Name each suite after the flow it tests and NEVER after a flow
file. `run_flow_tests <dir>` loads the whole folder for every suite, but a suite
run by name (`dolphin test x_test.yaml`, which is how CI runs the samples) loads
its companion `x.yaml` alone, without octo.yaml's connectors.
`sync-contacts_test.yaml` beside `contacts.yaml` runs against the folder either
way. A single-file draft (`<dir>/config.yaml` with
`config_test.yaml`) is fine until it outgrows one file.

## one-suite-per-flow — every flow with logic gets a suite

A suite runs one flow (`flow:`). Write `<flow>_test.yaml` for each flow that
branches, transforms or calls something. A caller's suite can still spy into a
subflow by its address (`close-won.set-won`).

## name-blocks — every block has a name

Kebab-case, verb-first: `find-contact`, `create-lead`, `post-note`. Names are what
logs print, what validate reports (`flow.block: ...`) and what mocks and spies
address. An unnamed block is addressed by its type, which breaks as soon as a
second block of that type joins its chain. Tool and slot sub-chains need names
too. A `ref:` step is addressed by its processor's name.

## cel-first — one expression before a foreach

`map`, `filter`, `exists`, ternaries: most reshaping is one CEL expression in a
`multi-transform` or `set-payload`. Keep `foreach` for calling something per item.
Multi-line CEL goes in a `|` block scalar.

## switch-over-ifs — three or more branches is a switch

Three or more branches on the same value: one `switch` with `cases` and `default`,
not nested `if`s.

## processors-ref — define a repeated block once

A block configured identically in several places becomes a `processors:` entry;
each use is `- ref: <name>`. Settings on the step override the processor's.

## env-config — configuration comes from env

Secrets, hosts, ports: `env` entries, `required: true` for secrets, defaults for
the rest, referenced as `${NAME}`. Never inline a key or URL.

## errors-deliberate — decide how each risky step fails

`handle-errors` to recover locally, a flow-level `error:` chain to recover the
flow, or `failOnError: false` plus an explicit check of the result on a block that
has it (check `blocks`). Letting it fail the flow is fine when intended — say so
in a comment.

## idempotent-side-effects — scheduled and retried work filters what is done

A cron flow runs again and a queue redelivers: select only work not yet done
(e.g. Odoo `["activity_ids", "=", false]`), so a second run changes nothing.

## comments-why — comment why, not what

`#` comments say why a step exists or is shaped this way. There is no
`description` field: a leaf block silently drops unknown keys, a composite fails
on them.

## Before you hand back a definition

- No two setters in a row.
- Every block named, including tool and slot sub-chains.
- No body saved to a var just to restore it.
- No flow past ~10 steps without a reason.
- A suite per flow with logic, named after the flow.
