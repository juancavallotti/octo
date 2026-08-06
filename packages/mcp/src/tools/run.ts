import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { OctoMcpConfig } from "../backend";
import type { RunHostPort, RunKey, RunStateLike } from "../run-host";
import type { NamespaceResolver } from "../namespace";
import { parseEnv } from "../env";
import { mockSpecSchema } from "../mocks";
import { errorResult, guard, jsonResult, textResult } from "../result";

/**
 * The run-control tools: check whether an integration can start, run it (returning
 * a test URL), stop it, and read its logs. Run host I/O is injected as a
 * {@link RunHostPort} so these are testable without spawning a real `octo` process.
 *
 * The three tools that address a long-running app all take the integration's `id`, and
 * that is not redundant with the session: a host that runs the app beside itself keys on
 * the session, while one that runs it elsewhere keys on (user, integration) and has
 * nothing else to find it by. It costs a caller nothing — you cannot have a run without
 * having called `run_integration` with that id.
 */
export function registerRunTools(
  server: McpServer,
  config: OctoMcpConfig,
  runHost: RunHostPort,
  resolveNamespace: NamespaceResolver,
): void {
  const { store } = config;

  /**
   * The run an addressed call refers to, assembled from the three things this request
   * knows: the MCP session, the integration named in the arguments, and the caller.
   *
   * The user comes off the verified bearer token (`AuthInfo.extra`), never from the
   * arguments — a host that scopes runs per user must not let one client name another's.
   * An unauthenticated host (the standalone app) has none, and keys on the session alone.
   */
  const runKeyOf = (
    extra: { sessionId?: string; authInfo?: { extra?: Record<string, unknown> } },
    integrationId?: string,
  ): RunKey => {
    const userId = extra.authInfo?.extra?.userId;
    return {
      namespace: resolveNamespace(extra.sessionId),
      integrationId,
      userId: typeof userId === "string" ? userId : undefined,
    };
  };

  server.registerTool(
    "can_start_integration",
    {
      title: "Can start integration",
      description:
        "Check whether an integration is ready to run: whether a runner is available and whether its definition validates. Returns { available, valid, errors }.",
      inputSchema: { id: z.string().min(1).describe("The integration id.") },
    },
    ({ id }) =>
      guard(async () => {
        const rec = await store.get(id);
        const { valid, errors } = await config.validate(rec.definition);
        return jsonResult({
          available: runHost.binaries().available,
          valid,
          errors,
        });
      }),
  );

  server.registerTool(
    "run_integration",
    {
      title: "Run integration",
      description:
        "Start (or restart) an integration in the dev runner, optionally injecting env vars. Returns the test URL to exercise a networked integration's HTTP endpoints. Use `get_run_logs` to see output and `stop_integration` to tear it down.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id to run."),
        env: z
          .record(z.string(), z.string())
          .optional()
          .describe("Optional env vars injected into the run (name → value)."),
      },
    },
    ({ id, env }, extra) =>
      guard(async () => {
        const rec = await store.get(id);
        const key = runKeyOf(extra, id);
        // Kept on a host whose app runs elsewhere too, where it is not strictly the right
        // question: the binary still backs invoke/evaluate_cel/run_tests, and every host
        // that mounts this server ships it. Reporting a run as unavailable because the
        // binary is missing is at worst pessimistic; the alternative — asking the host
        // "can you start an app?" — is a second flag for a case neither host has.
        if (!runHost.binaries().available) {
          return errorResult("Runner not available (OCTO_BIN_PATH unset).");
        }
        // We don't gate the run on `can_start_integration`'s validation: that
        // check is a best-effort pre-flight that can flag valid runtime YAML
        // (e.g. the processors/ref pattern). The runtime is the real judge —
        // its load errors stream to `get_run_logs`.
        let parsedEnv: Record<string, string> | undefined;
        if (env !== undefined) {
          const sane = parseEnv(env);
          if (!sane) return errorResult("invalid env (names must match [A-Za-z_][A-Za-z0-9_]* with string values)");
          parsedEnv = sane;
        }
        const st = await runHost.start(key, rec.definition, parsedEnv, {
          resources: config.resources?.(id),
        });
        const testUrl = buildTestUrl(config, st);
        return jsonResult({
          running: st.running,
          exposable: st.exposable,
          testUrl,
          namespace: key.namespace,
          note: st.exposable
            ? undefined
            : "Integration declares no HTTP_PORT, so it has no testable HTTP endpoint.",
        });
      }),
  );

  server.registerTool(
    "invoke_flow",
    {
      title: "Invoke a single flow",
      description:
        "Run ONE named flow once and return its result and logs — without starting the integration's sources. Supply either a saved integration `id` or an inline `definition` (raw runtime YAML), the `flow` name, and optional `data` (JSON request body), `vars` (JSON seeding the message variables), and `env`. This is the fast way to test a flow: returns { ok, timedOut, dropped, output, logs } where `output` is the flow's result MESSAGE as JSON text — `{event_id, variables, body}`, so read `body` for the payload and `variables` for what the flow built up along the way — `dropped` is true when the flow filtered the message (no result), and `logs` are the runner's stderr lines. Because no source runs, nothing populates the message for you — a flow that reads `vars` (e.g. HTTP headers a real request would have carried) needs them supplied via `vars`.\n\nThree debugging features can be combined on a run, each addressing blocks by the same path grammar — `<flow>.<block>`, descending into a composite with a bracketed branch: `orders.charge`, `orders.checkHeader[else].api-call`, `orders[error].notify`. Every segment but the last names a composite you are descending THROUGH, and must name a branch of it — even when that composite has one obvious chain (`weather.handle-errors[process].weather-step`, never `weather.weather-step`). A composite's branch names are listed in its `getSchema(<block type>)` entry under `addressBranches`; read that instead of guessing.\n\n• `breakAt` — run until this block, then stop. The result carries `breakpoint: { reached, block, message, error }`; `reached:false` means the flow never took that branch (a normal outcome, not an error).\n• `spies` — record every message crossing these blocks WITHOUT changing what the flow does. The result carries `spies: [{ address, records: [{ seq, at, input, output?, dropped?, error? }] }]`. A record shows what the block received and what it produced — including the two outcomes that are not a message: it dropped, or it failed. `seq` orders records across ALL spies, so a multi-spy trace reads as one timeline. An empty `records` list is a result, not a gap: the flow may simply have taken a branch that block is not on.\n• `mocks` — stand in for these blocks so the real one NEVER runs: the way to exercise a flow whose blocks call a payment API, an LLM, or anything else you don't want hit. Keyed by address.\n\nTwo mock rules the runtime enforces, both easy to get wrong. (1) A mock REPLACES its target, so a message matching no case FAILS the block — it does not fall through to the real block, because there is no real block left. Always give a `default`, or a trailing case with `when: \"true\"`, unless you mean an unmatched message to be an error. (2) A block either returns a message, fails, or drops it, so each case sets exactly ONE of `body` / `error` / `drop`; `vars` only goes alongside a `body`.\n\nAlso: you cannot `breakAt` or spy a block INSIDE a mocked block — the mock deleted that subtree, so nothing there can ever run, and the runtime rejects the whole request rather than silently reporting nothing. Use `list_flows` to discover flow names.",
      inputSchema: {
        id: z
          .string()
          .min(1)
          .optional()
          .describe("The saved integration id to run (mutually exclusive with `definition`)."),
        definition: z
          .string()
          .min(1)
          .optional()
          .describe("Inline runtime YAML to run unsaved (mutually exclusive with `id`)."),
        flow: z.string().min(1).describe("The name of the flow to invoke."),
        data: z
          .string()
          .optional()
          .describe("Optional JSON request body passed to the flow."),
        vars: z
          .string()
          .optional()
          .describe(
            "Optional JSON object seeding the message variables — what a source (e.g. the http one, copying request headers) would normally set.",
          ),
        env: z
          .record(z.string(), z.string())
          .optional()
          .describe("Optional env vars injected into the run (name → value)."),
        breakAt: z
          .string()
          .min(1)
          .optional()
          .describe(
            "Optional breakpoint address: run until this block, then stop and report the message it produced. Addressed as `<flow>.<block>`, descending into a composite with a bracketed branch — e.g. `orders.charge`, `orders.checkHeader[else].api-call`, `orders[error].notify`. A composite on the way to the target always needs a branch; `getSchema(<block type>)` lists the ones it has under `addressBranches`.",
          ),
        spies: z
          .array(z.string().min(1))
          .optional()
          .describe(
            "Optional block addresses to watch. Records every message crossing each one, without changing what the flow does. Returned as `spies`.",
          ),
        mocks: z
          .record(z.string(), mockSpecSchema)
          .optional()
          .describe(
            "Optional blocks to stand in for, keyed by address. The real block never runs — this is how a flow that calls a payment API or an LLM is exercised without one.",
          ),
        logLevel: z
          .enum(["debug", "info", "warn", "error"])
          .optional()
          .describe("Optional runner log level (default: the runtime's own, info)."),
        timeoutMs: z
          .number()
          .int()
          .positive()
          .optional()
          .describe("Max time to wait for the flow, in milliseconds (default 30000)."),
      },
    },
    (
      { id, definition, flow, data, vars, env, breakAt, spies, mocks, logLevel, timeoutMs },
      extra,
    ) =>
      guard(async () => {
        if ((id === undefined) === (definition === undefined)) {
          return errorResult("provide exactly one of id or definition");
        }
        const yaml =
          definition !== undefined ? definition : (await store.get(id!)).definition;
        const ns = resolveNamespace(extra.sessionId);
        if (!runHost.binaries().available) {
          return errorResult("Runner not available (OCTO_BIN_PATH unset).");
        }
        let parsedEnv: Record<string, string> | undefined;
        if (env !== undefined) {
          const sane = parseEnv(env);
          if (!sane) return errorResult("invalid env (names must match [A-Za-z_][A-Za-z0-9_]* with string values)");
          parsedEnv = sane;
        }
        const r = await runHost.invoke(ns, yaml, flow, {
          data,
          vars,
          env: parsedEnv,
          breakAt,
          spies,
          mocks,
          logLevel,
          timeoutMs,
          // `id` is undefined for an inline definition; the host decides what that
          // yields (the platform: no resources; standalone: its shared files).
          resources: config.resources?.(id),
        });
        return jsonResult({
          ok: r.ok,
          timedOut: r.timedOut,
          dropped: r.dropped,
          output: r.output,
          logs: r.logs,
          breakpoint: r.breakpoint,
          spies: r.spies,
        });
      }),
  );

  server.registerTool(
    "evaluate_cel",
    {
      title: "Evaluate a CEL expression",
      description:
        "Evaluate a single CEL expression against a JSON object WITHOUT running a flow — the fast way to test transform, filter, or routing logic. `expression` is the CEL to evaluate; `data` (JSON) is bound to `body`, `vars` (JSON) to `vars`, and `env` to the `env` map — the same variables a flow's message expressions see (body, vars, env, eventID, correlationID, now). Returns { ok, result, error }: on success `ok` is true and `result` holds the evaluated value (which may itself be false, 0, or null); a compile or evaluation failure returns `ok:false` with the message in `error`. Note: `templateResource()` and a real integration's resolved env are not available here — pass any needed values via `env`/`vars`.",
      inputSchema: {
        expression: z
          .string()
          .min(1)
          .describe("The CEL expression to evaluate."),
        data: z
          .string()
          .optional()
          .describe("Optional JSON object bound to `body`."),
        vars: z
          .string()
          .optional()
          .describe("Optional JSON object bound to `vars`."),
        env: z
          .record(z.string(), z.string())
          .optional()
          .describe("Optional map bound to the CEL `env` variable (name → value)."),
        timeoutMs: z
          .number()
          .int()
          .positive()
          .optional()
          .describe("Max time to wait for evaluation, in milliseconds (default 10000)."),
      },
    },
    ({ expression, data, vars, env, timeoutMs }, extra) =>
      guard(async () => {
        // Reuse the per-session availability gate for a consistent "runner configured?"
        // error, even though eval itself is stateless (no namespace isolation needed).
        const ns = resolveNamespace(extra.sessionId);
        if (!runHost.binaries().available) {
          return errorResult("Runner not available (OCTO_BIN_PATH unset).");
        }
        let parsedEnv: Record<string, string> | undefined;
        if (env !== undefined) {
          const sane = parseEnv(env);
          if (!sane) return errorResult("invalid env (names must match [A-Za-z_][A-Za-z0-9_]* with string values)");
          parsedEnv = sane;
        }
        const r = await runHost.evalCel(expression, {
          data,
          vars,
          env: parsedEnv,
          timeoutMs,
        });
        return jsonResult({ ok: r.ok, result: r.result, error: r.error });
      }),
  );

  server.registerTool(
    "stop_integration",
    {
      title: "Stop integration",
      description:
        "Stop the running integration and free its resources. Pass the same `id` you started it with. Returns { running: false }.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id whose run to stop."),
      },
    },
    ({ id }, extra) =>
      guard(async () => {
        const st = await runHost.stop(runKeyOf(extra, id));
        return jsonResult({ running: st.running });
      }),
  );

  server.registerTool(
    "get_run_logs",
    {
      title: "Get run logs",
      description:
        "Read the running integration's output (oldest line first), as plain text. Pass the same `id` you started it with.",
      inputSchema: {
        id: z.string().min(1).describe("The integration id whose run to read."),
      },
    },
    ({ id }, extra) =>
      guard(async () => {
        const lines = await runHost.snapshot(runKeyOf(extra, id));
        if (lines.length === 0) {
          return textResult("(no logs yet — start a run with run_integration)");
        }
        return textResult(lines.map((l) => l.text).join("\n"));
      }),
  );
}

/**
 * The absolute URL of a run, or null when it serves no HTTP.
 *
 * The host reports an app-relative path when it proxies to its own child process, and an
 * absolute URL when the run has a hostname of its own. Only the first needs a base, and
 * prefixing the second would produce `http://localhost:3000https://…` — so which kind it
 * is has to be checked rather than assumed.
 */
function buildTestUrl(config: OctoMcpConfig, st: RunStateLike): string | null {
  if (!st.testUrl) return null;
  if (/^https?:\/\//i.test(st.testUrl)) return st.testUrl;
  const base = config.baseUrl?.replace(/\/+$/, "");
  return base ? `${base}${st.testUrl}` : st.testUrl;
}
