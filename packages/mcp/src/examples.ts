/**
 * Worked integration definitions surfaced through the `integration-examples`
 * prompt. They are trimmed, runnable versions of the repo's `samples/`, chosen to
 * show the two shapes a consumer LLM most needs: a timer-driven (internal) flow and
 * a networked (HTTP-triggered, testable) flow.
 */

export interface Example {
  title: string;
  /** What the example demonstrates, one line. */
  summary: string;
  /** A complete runtime-YAML definition. */
  definition: string;
}

/** A cron-triggered flow that logs — internal, not networked (no test URL). */
const HELLO_WORLD: Example = {
  title: "hello-world — cron source → log",
  summary:
    "Fires on a schedule and logs a line. Internal only (no HTTP_PORT), so it has no test URL — watch it via get_run_logs.",
  definition: `service:
  name: hello-world

connectors:
  - name: ticker
    type: cron

# Named processor definitions; flow blocks reference these by \`ref\`.
processors:
  - name: greeter
    type: log
    settings:
      level: info
      # CEL expression rendered to the log line (sees body/vars/eventID).
      message: '"hello world! the date is " + body.date'

flows:
  - name: greet
    source:
      connector: ticker
      type: cron
      settings:
        schedule: "0,30 * * * * *"      # second 0 and 30 of every minute
        payload: '{"date": string(now)}'
    process:
      - ref: greeter
`,
};

/**
 * An HTTP-triggered flow. Declaring HTTP_PORT makes the run networked, so
 * run_integration returns a test URL that proxies to this endpoint.
 */
const HTTP_ORDERS: Example = {
  title: "http-orders — HTTP source → log (networked, testable)",
  summary:
    "Declares HTTP_PORT and an `http` connector, so the run is networked and run_integration returns a test URL. POST to <testUrl>orders/42 to exercise it.",
  definition: `service:
  name: http-orders

# Declare env before referencing it as \${NAME}. HTTP_PORT makes the run networked.
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
  - name: debug
    type: logger
    settings:
      format: json
      level: debug

flows:
  - name: orders-api
    source:
      connector: api
      type: http
      settings:
        path: /orders/{id}            # {id} -> vars.id
    process:
      - type: log
        name: dump-inbound
        settings:
          logger: debug
          level: debug
          full: true                  # dump the whole message as it arrives
`,
};

export const EXAMPLES: Example[] = [HELLO_WORLD, HTTP_ORDERS];
