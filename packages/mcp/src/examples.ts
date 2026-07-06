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
const QUEUE_LOADBALANCE: Example = {
  slug: "queue-loadbalance",
  title: "queue-loadbalance — platform queue source + queue-dispatch (load balancing)",
  summary:
    "An HTTP-triggered flow hands work to worker flows through platform queue subjects: the default fire-and-forget publish (audit) and an awaitReply request (enrich-work, whose reply folds back). Each worker is a `queue` source on the subject; scale replicas to load balance (competing consumers, each message handled once). Declares HTTP_PORT, so run_integration returns a test URL: POST <testUrl>orders/42.",
  blocks: ["http (source)", "queue (source)", "queue-dispatch", "if", "set-variable", "set-payload", "log"],
  definition: `service:
  name: queue-loadbalance

# queue-dispatch is the cross-replica analogue of flow-ref: request waits for one
# competing consumer's reply and folds it back; oneWay publishes fire-and-forget.
# Each worker flow is a \`queue\` source on the matching subject.
env:
  - name: HTTP_HOST
    default: 0.0.0.0
  - name: HTTP_PORT
    default: "8080"
  - name: HTTP_BASE_PATH
    default: /api/v1

connectors:
  - name: api
    type: http
    settings:
      host: \${HTTP_HOST}
      port: \${HTTP_PORT}
      basePath: \${HTTP_BASE_PATH}
      requestTimeout: 5s
  - name: debug
    type: logger
    settings:
      format: json
      level: debug
  # No queue connector is declared: it has no global config, so a queue source (or
  # the queue-dispatch block) resolves it implicitly — the runtime starts a default
  # instance on demand. The queue is a core runtime service.

flows:
  - name: orders-api
    source:
      connector: api
      type: http
      settings:
        path: /orders/{id}
        correlationIdHeader: X-Request-Id
        headers: [X-Tenant]
        timeout: 5s
    process:
      # Default: fire-and-forget publish, the caller does not wait. Subject is CEL.
      - type: queue-dispatch
        name: audit-async
        settings:
          subject: '"audit-work"'
      # awaitReply: wait for one worker reply; body + variables fold back.
      - type: queue-dispatch
        name: enrich-sync
        settings:
          subject: '"enrich-work"'
          awaitReply: true
      - type: log
        settings:
          message: '"responded to order " + vars.id + " priority=" + vars.priority'

  # Competing consumer on enrich-work; its final message is returned as the reply.
  - name: order-enricher
    source:
      type: queue
      settings:
        subject: enrich-work
        listeners: 8
    process:
      - type: set-payload
        name: normalize-order
        settings:
          value: >
            {
              "orderId":   vars.id,
              "tenant":    vars["X-Tenant"],
              "item":      body.item,
              "amount":    body.amount,
              "requestId": correlationID
            }
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

  # Competing consumer on audit-work; invoked one-way, so its result is discarded.
  - name: order-auditor
    source:
      type: queue
      settings:
        subject: audit-work
    process:
      - type: log
        name: audit-log
        settings:
          logger: debug
          level: info
          message: '"AUDIT order " + vars.id + " req=" + correlationID'
`,
};

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

/** A Slack bot: verify inbound events over HTTP and reply to @-mentions. */
const SLACK_BOT: Example = {
  slug: "slack-bot",
  title: "slack-bot — receive Slack events, verify, and reply",
  summary:
    "Slack posts events to an http route. slack-verify-request checks the signature over the raw body (note the source's headers + rawBodyVar); an if branch echoes Slack's URL-verification challenge, otherwise slack-event filters to @-mentions, slack-lookup-user resolves the mentioning user by id (by: id), and slack-send-message replies by name. Needs SLACK_BOT_TOKEN and SLACK_SIGNING_SECRET; test at /slack/events.",
  blocks: [
    "http (source)",
    "slack-verify-request",
    "if",
    "set-payload",
    "slack-event",
    "slack-lookup-user",
    "slack-send-message",
    "slack (connector)",
  ],
  definition: `service:
  name: slack-bot

env:
  - name: SLACK_BOT_TOKEN
    required: true
  - name: SLACK_SIGNING_SECRET
    required: true

connectors:
  - name: api
    type: http
    settings:
      port: 8080
  - name: slack
    type: slack
    settings:
      botToken: \${SLACK_BOT_TOKEN}
      signingSecret: \${SLACK_SIGNING_SECRET}

flows:
  - name: slack-events
    source:
      connector: api
      type: http
      settings:
        path: /slack/events
        # Copy the signature headers and the exact bytes so the HMAC can be
        # verified over the raw request body.
        headers: [X-Slack-Signature, X-Slack-Request-Timestamp]
        rawBodyVar: rawBody
    process:
      - type: slack-verify-request        # authenticate; flag the URL handshake
        settings:
          connector: slack
      - type: if                          # composite fields sit at the top level
        condition: has(vars.slackChallenge)
        then:
          process:
            - type: set-payload           # echo Slack's challenge -> 200
              settings:
                value: '{"challenge": body.challenge}'
        else:
          process:
            - type: slack-event           # keep only @-mentions from humans
              settings:
                eventTypes: [app_mention]
                filter: body.botId == null
            - type: slack-lookup-user     # resolve the user id -> vars.slackUser
              settings:
                connector: slack
                by: id
                user: body.user
            - type: slack-send-message    # reply in the same channel, by name
              settings:
                connector: slack
                target: body.channel
                text: '"thanks " + vars.slackUser.real_name + ", you said: " + body.text'
`,
};

/** The enrich scope: run a body on an isolated clone, propagate via expressions. */
const ENRICH: Example = {
  slug: "enrich",
  title: "enrich — isolated scope with expression-based propagation",
  summary:
    "Runs a body flow on a clone of the message, then enriches the original from the scope's result via CEL: setBody for the body and setVars (name -> expression) for variables, both evaluated against the enriched clone. Here the scope computes a summary body but only the total escapes via setVars; setBody is omitted so the original body is preserved. These fields sit at the block top level, not under settings.",
  blocks: ["enrich", "set-variable", "set-payload", "log"],
  definition: `service:
  name: enrich

connectors:
  - name: out
    type: logger
    settings:
      format: json
      level: info

flows:
  - name: order
    process:
      - type: enrich
        name: derive-total
        # setBody omitted: the incoming order body is preserved.
        setVars:
          total: body.total     # pull just the total out of the scope's summary
        body:
          process:
            - type: set-variable
              settings:
                name: workingNote
                value: '"scope-only, never propagated"'
            - type: set-payload
              settings:
                value: '{"total": body.qty * body.price, "note": "scope-only body"}'
      - type: log
        settings:
          logger: out
          message: '"order " + body.orderId + " total=" + string(vars.total)'
`,
};

/** The validate filter: assert CEL rules, stop the flow with a response on failure. */
const VALIDATE: Example = {
  slug: "validate",
  title: "validate — filter that rejects a request and stops the flow",
  summary:
    "A filter block: every CEL rule must hold or the block rejects the message — it stops the flow (the rest of the chain never runs) and returns a response it configures. With no onReject sub-flow it emits a built-in body {error, messages} with rejectStatus (default 422); the failing rules' messages are also exposed as vars.validationErrors. The commented onReject slot shows how to shape the rejection yourself (set body + vars.httpStatus). rules/rejectStatus/onReject sit at the block top level, not under settings. Declares HTTP_PORT, so run_integration returns a test URL: POST a body missing id or with amount<=0 to see the 422, or a valid one to see it accepted.",
  blocks: ["http (source)", "validate", "set-payload"],
  definition: `service:
  name: validate-orders

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
      port: \${HTTP_PORT}

flows:
  - name: orders-api
    source:
      connector: api
      type: http
      settings:
        path: /orders
    process:
      # Filter: all rules must hold, or the request is rejected and the flow stops.
      - type: validate
        name: check-order
        rejectStatus: 422              # status for the built-in rejection response
        rules:
          - expr: 'has(body.id)'
            message: "order id is required"
          - expr: 'body.amount > 0'
            message: "amount must be positive"
        # onReject (optional) shapes the response yourself instead of the built-in
        # {error, messages} body; it can read vars.validationErrors. Omit for the default.
        # onReject:
        #   process:
        #     - type: set-payload
        #       settings:
        #         value: '{"error": "bad_request", "details": vars.validationErrors}'
        #     - type: set-variable
        #       settings: { name: httpStatus, value: 400 }

      # Only reached when every rule held (a rejected request stopped here).
      - type: set-payload
        name: accept
        settings:
          value: '{"orderId": body.id, "amount": body.amount, "status": "accepted"}'
`,
};

/** The object store: write a value, read it back with a presence flag + default. */
const OBJECT_STORE: Example = {
  slug: "object-store",
  title: "object-store — object-write / object-read with default + objectExists",
  summary:
    "Persists a value with object-write and reads it back with object-read. Setting existsVar makes object-read write a boolean (true on a hit), and its `default` CEL expression is folded in on a miss (into `as`, or the body) so the flow never handles a null. The in-process standalone store is per-run, so this flow reads the key before and after writing it to show both states.",
  blocks: ["object-read", "object-write", "log"],
  definition: `service:
  name: object-store

connectors:
  - name: out
    type: logger
    settings:
      format: json
      level: info

flows:
  - name: profile
    process:
      # Cold read: key absent -> default folded in, objectExists false.
      - type: object-read
        settings:
          key: '"profile:" + body.id'
          as: profile
          default: '{"plan": "free"}'
          existsVar: profileExists
      - type: log
        settings:
          logger: out
          message: '"before: exists=" + string(vars.profileExists) + " plan=" + vars.profile.plan'
      - type: object-write
        settings:
          key: '"profile:" + body.id'
          value: '{"plan": body.plan}'
      # Warm read: key now exists -> stored value wins, profileExists true.
      - type: object-read
        settings:
          key: '"profile:" + body.id'
          as: profile
          default: '{"plan": "free"}'
          existsVar: profileExists
      - type: log
        settings:
          logger: out
          message: '"after: exists=" + string(vars.profileExists) + " plan=" + vars.profile.plan'
`,
};

/** Broadcast pub/sub: one publisher, multiple subscribers each get every message. */
const EVENTS: Example = {
  slug: "events",
  title: "events — broadcast pub/sub (publish-event + event source)",
  summary:
    "publish-event broadcasts the current message to a topic subject; every flow fronted by an `events` source on that subject receives its own copy — fan-out, unlike a queue where each message is handled once. Here a cron-driven flow publishes and two subscriber flows both receive every message. The `events` source resolves implicitly by type (no connector instance needed).",
  blocks: ["publish-event", "events (source)", "cron (source)", "log"],
  definition: `service:
  name: events

connectors:
  - name: out
    type: logger
    settings:
      format: json
      level: info

flows:
  - name: emitter
    source:
      type: cron
      settings:
        schedule: "@every 3s"
        payload: '{"tick": string(now)}'
    process:
      - type: publish-event
        settings:
          subject: '"notifications"'   # CEL subject; constant here
  - name: subscriber-a
    source:
      type: events                     # implicit connector — no instance needed
      settings:
        subject: notifications
    process:
      - type: log
        settings:
          logger: out
          message: '"A saw tick " + body.tick'
  - name: subscriber-b
    source:
      type: events
      settings:
        subject: notifications
    process:
      - type: log
        settings:
          logger: out
          message: '"B saw tick " + body.tick'
`,
};

/** An ordered chain of additive CEL edits collapsed into one block. */
const MULTI_TRANSFORM: Example = {
  slug: "multi-transform",
  title: "multi-transform — additive edits in one block",
  summary:
    "Applies an ordered list of CEL edits in one block: each step either sets the body (setBody) or a variable (setVar/value), and the edits accumulate so a later step reads what an earlier one produced. Collapses a chain of set-payload / set-variable blocks. The transforms list sits under settings.",
  blocks: ["multi-transform", "log"],
  definition: `service:
  name: multi-transform

connectors:
  - name: out
    type: logger
    settings:
      format: json
      level: info

flows:
  - name: order
    process:
      - type: multi-transform
        name: price-order
        settings:
          transforms:
            - setBody: '{"orderId": body.orderId, "subtotal": body.qty * body.price}'
            - setVar: subtotal
              value: body.subtotal
            - setBody: '{"orderId": body.orderId, "subtotal": vars.subtotal, "total": vars.subtotal * 1.1}'
      - type: log
        settings:
          logger: out
          message: '"order " + body.orderId + " total=" + string(body.total)'
`,
};

/** The ai-agent with per-thread memory, plus clearing a thread. */
const AI_AGENT_MEMORY: Example = {
  slug: "ai-agent-memory",
  title: "ai-agent-memory — a stateful agent with per-thread memory",
  summary:
    "An ai-agent with memoryThreadId loads a thread's prior transcript before its run and saves it after, so conversations persist across invocations; memoryMaxTokens + memoryCompaction (prune|summarize) bound it. The clear-agent-memory block wipes a thread. Needs ANTHROPIC_API_KEY. The agent's fields (connector/prompt/tools/memory*) sit at the block top level.",
  blocks: ["ai-agent", "clear-agent-memory", "set-variable", "set-payload"],
  definition: `service:
  name: ai-agent-memory

env:
  - name: ANTHROPIC_API_KEY
    required: true

connectors:
  - name: claude
    type: llm-anthropic
    settings:
      apiKey: \${ANTHROPIC_API_KEY}

flows:
  - name: chat
    process:
      - type: ai-agent
        name: assistant
        connector: claude
        memoryThreadId: body.threadId   # CEL: the conversation thread id
        memoryMaxTokens: 4000
        memoryCompaction: summarize
        prompt: >
          You are a helpful assistant in an ongoing conversation. Use the prior
          turns for context and answer the latest message. Respond with JSON
          {"reply": "..."}.
        tools:
          - name: remember_note
            description: Save a short note to scratch state for later in this task.
            inputSchema: |
              {"type":"object","required":["note"],"properties":{"note":{"type":"string"}}}
            process:
              - type: set-variable
                settings:
                  name: note
                  value: body.note

  - name: forget
    process:
      - type: clear-agent-memory
        settings:
          threadId: body.threadId
      - type: set-payload
        settings:
          value: '{"cleared": true}'
`,
};

/** An ai-agent that loads instruction resources as skills on demand. */
const AI_AGENT_SKILLS: Example = {
  slug: "ai-agent-skills",
  title: "ai-agent-skills — load instruction resources as skills",
  summary:
    "An ai-agent with a `skills` list: each skill binds a name + description (shown to the model up front) to a template resource. The agent calls the implicit load_skill tool to pull a skill's full content into the conversation only when relevant, keeping the base prompt small. The skill bodies are declared under resources.templates (their own files). Needs ANTHROPIC_API_KEY. The agent's fields (connector/prompt/tools/skills) sit at the block top level.",
  blocks: ["ai-agent", "set-payload"],
  definition: `service:
  name: ai-agent-skills

env:
  - name: ANTHROPIC_API_KEY
    required: true

# Skill bodies are template resources referenced by their \`as\` alias.
resources:
  templates:
    - resource: skills/refunds.md
      as: refunds
    - resource: skills/shipping.md
      as: shipping

connectors:
  - name: claude
    type: llm-anthropic
    settings:
      apiKey: \${ANTHROPIC_API_KEY}

flows:
  - name: support
    process:
      - type: ai-agent
        name: support-assistant
        connector: claude
        prompt: >
          You are a customer-support assistant. Load the relevant skill to ground
          your answer in policy, then respond with JSON {"answer": "..."}.
        # Only name + description are in the prompt up front; load_skill(name)
        # fetches the body on demand.
        skills:
          - name: refunds
            description: How to decide and explain refunds by order age and type.
            resource: refunds
          - name: shipping
            description: Shipping options, coverage, tracking, and lost-parcel claims.
            resource: shipping
        tools:
          - name: lookup_order
            description: Look up an order's delivery date and status by id.
            inputSchema: |
              {"type":"object","required":["orderId"],"properties":{"orderId":{"type":"string"}}}
            process:
              - type: set-payload
                settings:
                  value: '{"orderId":"A-100","deliveredDaysAgo":14,"type":"physical"}'
`,
};

/** Produce and serve non-JSON payloads with raw-content mode + form conversion. */
const RAW_CONTENT: Example = {
  slug: "raw-content",
  title: "raw-content — serve HTML + parse a form (raw-content mode)",
  summary:
    "Produces a typed non-JSON body. set-payload with rawBody+contentType writes {contentType, rawData}, which the http source serves verbatim (text/html, not JSON). A second flow accepts a non-JSON request via the source rawBody setting and reverts it with fromFormData(body.rawData). Declares HTTP_PORT, so run_integration returns a test URL: GET <testUrl>page/World, POST <testUrl>submit.",
  blocks: ["http (source)", "set-payload"],
  definition: `service:
  name: raw-content

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
      port: \${HTTP_PORT}

flows:
  # PRODUCE + SERVE raw content. rawBody: true stores the value (which must be a
  # string) as {contentType, rawData}; the http source serves rawData verbatim
  # with that Content-Type instead of JSON-encoding the body.
  - name: page
    source:
      connector: api
      type: http
      settings:
        path: /page/{name}            # {name} -> vars.name
    process:
      - type: set-payload
        settings:
          rawBody: true
          contentType: text/html; charset=utf-8
          value: '"<h1>Hello, " + vars.name + "!</h1>"'

  # SOURCE raw content, then revert it. The source rawBody accepts any content
  # type and stores it as {contentType, rawData}; fromFormData parses the raw
  # urlencoded form back into a structured body, returned as JSON.
  - name: submit
    source:
      connector: api
      type: http
      settings:
        path: /submit
        rawBody: true
    process:
      - type: set-payload
        settings:
          value: 'fromFormData(body.rawData)'
`,
};

/** Serve a flow as a stateless MCP server with the mcp-router block. */
const MCP_ROUTER: Example = {
  slug: "mcp-router",
  title: "mcp-router — serve a flow as an MCP server",
  summary:
    "The inverse of Octo's authoring MCP server: the mcp-router block sits behind an HTTP source and turns this flow into a stateless MCP server. Each POST to /mcp is one MCP JSON-RPC request and the flow's output is the response. It advertises tool flows as MCP tools, template resources as MCP resources, and template resources as MCP prompts, and routes tools/call to the matching flow — no LLM. Declares HTTP_PORT, so run_integration returns a test URL. The tool's arguments become the branch body; resource/prompt bodies come from resources.templates (their own files). The block's fields (serverName/tools/resources/prompts) sit at the block top level.",
  blocks: ["http (source)", "mcp-router", "set-payload"],
  definition: `service:
  name: mcp-router

env:
  - name: HTTP_PORT
    default: "8080"

# Template resources advertised as MCP resources and prompts (their bodies live
# in their own files, referenced here by alias).
resources:
  templates:
    - resource: guide.md
      as: guide
    - resource: greet.md
      as: greet

connectors:
  - name: api
    type: http
    settings:
      port: \${HTTP_PORT}

flows:
  - name: mcp
    source:
      connector: api
      type: http
      settings:
        path: /mcp
    process:
      - type: mcp-router
        name: weather-mcp
        serverName: weather-tools
        tools:
          - name: forecast
            description: Return a short weather forecast for a city.
            inputSchema: |
              {"type":"object","required":["city"],"properties":{"city":{"type":"string"}}}
            process:
              - type: set-payload
                settings:
                  value: '{"city": body.city, "summary": "Sunny, 24C"}'
        resources:
          - uri: octo://guide
            name: guide
            description: Operator guide for the weather tools.
            mimeType: text/markdown
            resource: guide
        prompts:
          - name: assist
            description: Prime an assistant to help a user in a given city.
            arguments:
              - name: city
                required: true
            resource: greet
`,
};

export const EXAMPLES: Example[] = [
  HELLO_WORLD,
  BUILTINS,
  HTTP_ORDERS,
  QUEUE_LOADBALANCE,
  ERROR_HANDLING,
  AI_ROUTER,
  SLACK_BOT,
  ENRICH,
  VALIDATE,
  OBJECT_STORE,
  MULTI_TRANSFORM,
  EVENTS,
  AI_AGENT_MEMORY,
  AI_AGENT_SKILLS,
  MCP_ROUTER,
  RAW_CONTENT,
];

/** The example whose slug matches, or undefined. */
export function getExample(slug: string): Example | undefined {
  return EXAMPLES.find((e) => e.slug === slug);
}
