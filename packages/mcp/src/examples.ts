/**
 * Worked integration definitions, served as `octo://examples/<slug>` resources so
 * a consumer LLM can learn idiomatic block usage instead of guessing. They are
 * faithful (trimmed) versions of the repo's `samples/`, each tagged with the
 * blocks/connectors it demonstrates so the model can pick the right one. Note that
 * composite blocks (if/switch/foreach/handle-errors/ai-router/flow-ref) carry
 * their sub-fields at the block's top level, not under `settings`.
 */

export interface Example {
  /** URL-safe id; the resource is published at `octo://examples/<slug>`. */
  slug: string;
  title: string;
  /** What the example demonstrates, one line. */
  summary: string;
  /** Block and connector types this example shows in use. */
  blocks: string[];
  /** A complete runtime-YAML definition. */
  definition: string;
}

/** A cron-triggered flow that logs — the smallest runnable integration. */
const HELLO_WORLD: Example = {
  slug: "hello-world",
  title: "hello-world — cron source → log",
  summary:
    "The smallest integration: fire on a schedule and log a line. Internal (no HTTP_PORT), so no test URL — watch it via get_run_logs.",
  blocks: ["cron (source)", "log"],
  definition: `service:
  name: hello-world

connectors:
  - name: ticker
    type: cron

flows:
  - name: greet
    source:
      connector: ticker
      type: cron
      settings:
        schedule: "0,30 * * * * *"      # second 0 and 30 of every minute
        payload: '{"date": string(now)}'
    process:
      - type: log
        name: greeter
        settings:
          level: info
          # CEL expression rendered to the log line (sees body/vars/eventID).
          message: '"hello world! the date is " + body.date'
`,
};

/** Control-flow + data-shaping builtins in one cron-driven flow. */
const BUILTINS: Example = {
  slug: "builtins",
  title: "builtins — set-payload, set-variable, if, foreach, switch",
  summary:
    "Shapes a payload, stashes a variable, then branches (if/else), iterates (foreach), and classifies (switch). Note the composite blocks' fields (condition/then/else, items/as/body, cases/default) sit at the block top level.",
  blocks: [
    "set-payload",
    "set-variable",
    "delete-variable",
    "if",
    "foreach",
    "switch",
    "log",
  ],
  definition: `service:
  name: builtins-demo

connectors:
  - name: ticker
    type: cron

flows:
  - name: demo
    source:
      connector: ticker
      type: cron
      settings:
        schedule: "@every 3s"
        payload: '{"firedAt": string(now)}'
    process:
      # set-payload: replace the body with the result of a CEL expression.
      - type: set-payload
        name: seed-orders
        settings:
          value: '{"orders": [{"id": 1, "amount": 50}, {"id": 2, "amount": 250}]}'

      # set-variable: stash a value the switch below compares against.
      - type: set-variable
        name: set-threshold
        settings:
          name: threshold
          value: "100"

      # if/else: composite fields (condition/then/else) are top-level.
      - type: if
        name: any-orders
        condition: "size(body.orders) > 0"
        then:
          process:
            - type: log
              settings:
                message: '"processing " + string(size(body.orders)) + " orders"'
        else:
          process:
            - type: log
              settings:
                message: '"no orders to process"'

      # foreach: bind each element to \`order\`; nested switch classifies it.
      - type: foreach
        name: each-order
        items: "body.orders"
        as: order
        body:
          process:
            - type: switch
              name: classify-order
              cases:
                - when: "vars.order.amount >= vars.threshold"
                  process:
                    - type: log
                      settings:
                        message: '"HIGH order " + string(vars.order.id)'
              default:
                process:
                  - type: log
                    settings:
                      message: '"low order " + string(vars.order.id)'

      # delete-variable: clean up the scratch variable.
      - type: delete-variable
        name: drop-threshold
        settings:
          name: threshold
`,
};

/** HTTP API that composes sub-flows via flow-ref (sync + one-way). Networked. */
const HTTP_ORDERS: Example = {
  slug: "http-orders",
  title: "http-orders — HTTP source + flow-ref composition (networked)",
  summary:
    "An HTTP-triggered flow that fans out to sourceless flows with flow-ref — one-way (fire-and-forget audit) and sync (enrich-order, whose result folds back). Declares HTTP_PORT, so run_integration returns a test URL: POST <testUrl>orders/42.",
  blocks: [
    "http (source)",
    "flow-ref",
    "switch",
    "if",
    "set-variable",
    "set-payload",
    "log",
  ],
  definition: `service:
  name: http-orders

# Declare env before referencing it as \${NAME}. HTTP_PORT makes the run networked.
env:
  - name: HTTP_HOST
    default: 0.0.0.0
  - name: HTTP_PORT
    default: "8080"

connectors:
  - name: api
    type: http
    settings:
      host: \${HTTP_HOST}
      port: \${HTTP_PORT}             # an exact \${VAR} keeps its type -> int 8080

flows:
  - name: orders-api
    source:
      connector: api
      type: http
      settings:
        path: /orders/{id}            # {id} -> vars.id
    process:
      # ONE-WAY flow-ref: fire-and-forget; we don't wait for \`audit\`.
      - type: flow-ref
        name: audit-async
        settings:
          flow: audit
          oneWay: true

      # Content-based routing on the HTTP method.
      - type: switch
        name: route-by-method
        cases:
          - when: 'vars.method == "POST"'
            process:
              # SYNC flow-ref (oneWay defaults false): wait, fold result back in.
              - type: flow-ref
                name: enrich-sync
                settings:
                  flow: enrich-order
        default:
          process:
            - type: set-payload
              settings:
                value: '{"orderId": vars.id, "status": "found"}'

  # Sourceless flow (no \`source:\`) -> callable by name. Invoked one-way here.
  - name: audit
    process:
      - type: log
        settings:
          message: '"AUDIT order " + vars.id + " req=" + correlationID'

  # Sourceless flow invoked synchronously; its body/vars fold back into the caller.
  - name: enrich-order
    process:
      - type: set-payload
        name: normalize-order
        settings:
          value: '{"orderId": vars.id, "item": body.item, "amount": body.amount}'
      - type: if
        name: priority-check
        condition: 'body.amount >= 1000.0'
        then:
          process:
            - type: set-variable
              settings: { name: priority, value: '"high"' }
        else:
          process:
            - type: set-variable
              settings: { name: priority, value: '"normal"' }
      - type: set-payload
        name: wrap-response
        settings:
          value: '{"order": body, "priority": vars.priority, "status": "accepted"}'
`,
};

/** Error recovery: inline handle-errors and a flow-level error chain. Networked. */
const ERROR_HANDLING: Example = {
  slug: "error-handling",
  title: "error-handling — handle-errors, rest, flow-level error path",
  summary:
    "Two resilience patterns around a failing outbound `rest` call: inline recovery with handle-errors (process + error chains), and a flow-level `error:` chain that sets vars.httpStatus and a degraded body. vars.error = { message, flow, block }.",
  blocks: ["handle-errors", "rest", "set-payload", "set-variable", "http-client (connector)"],
  definition: `service:
  name: error-handling

connectors:
  - name: api
    type: http
    settings:
      port: 8080
  - name: payments
    type: http-client
    settings:
      baseURL: http://127.0.0.1:9     # discard port -> every call fails to connect

flows:
  # Inline recovery: handle-errors runs \`process\`, and on failure runs \`error\`,
  # which sees the failure as vars.error. The flow then completes normally (200).
  - name: charge-inline
    source:
      connector: api
      type: http
      settings:
        path: /inline
    process:
      - type: handle-errors
        name: charge
        process:
          - type: rest
            name: call-charge
            settings:
              connector: payments
              method: POST
              path: /charges
              body: '{"amount": body.amount}'
        error:
          - type: set-payload
            settings:
              value: '{"status": "degraded", "reason": vars.error.message}'

  # Flow-level error path: no handle-errors, so a failing block redirects the
  # message to the flow \`error:\` chain (which can set the HTTP status).
  - name: charge-flowlevel
    source:
      connector: api
      type: http
      settings:
        path: /flowlevel
    process:
      - type: rest
        name: call-charge
        settings:
          connector: payments
          method: POST
          path: /charges
          body: '{"amount": body.amount}'
    error:
      - type: set-variable
        settings: { name: httpStatus, value: "502" }
      - type: set-payload
        settings:
          value: '{"error": vars.error.message, "failedBlock": vars.error.block}'
`,
};

/** An LLM picks one of several named routes per message. */
const AI_ROUTER: Example = {
  slug: "ai-router",
  title: "ai-router — LLM routes each message to a named branch",
  summary:
    "The ai-router block hands the message to an LLM connector, which picks one of the described routes (or the default guardrail). Needs ANTHROPIC_API_KEY. Sourceless flow, so drive it with run_integration + the runtime's invoke.",
  blocks: ["ai-router", "set-payload", "llm-anthropic (connector)"],
  definition: `service:
  name: ai-router

env:
  - name: ANTHROPIC_API_KEY
    required: true

connectors:
  - name: claude
    type: llm-anthropic
    settings:
      apiKey: \${ANTHROPIC_API_KEY}

flows:
  - name: triage
    process:
      - type: ai-router
        name: triage-ticket
        connector: claude
        prompt: >
          Read the support ticket in the message body and route it to the team
          best suited to handle it.
        guardrail: >
          If the ticket is ambiguous or you are not confident, take the default
          (human triage) path.
        routes:
          - name: billing
            description: Payment failures, refunds, invoices, subscription changes.
            process:
              - type: set-payload
                settings:
                  value: '{"team": "billing"}'
          - name: technical
            description: Bugs, outages, API errors, integration problems.
            process:
              - type: set-payload
                settings:
                  value: '{"team": "technical"}'
        default:
          process:
            - type: set-payload
              settings:
                value: '{"team": "human-triage"}'
`,
};

export const EXAMPLES: Example[] = [
  HELLO_WORLD,
  BUILTINS,
  HTTP_ORDERS,
  ERROR_HANDLING,
  AI_ROUTER,
];

/** The example whose slug matches, or undefined. */
export function getExample(slug: string): Example | undefined {
  return EXAMPLES.find((e) => e.slug === slug);
}
