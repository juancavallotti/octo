import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { OctoMcpConfig } from "../backend";
import type { RunHostPort, RunStatusLike } from "../run-host";
import type { NamespaceResolver } from "../namespace";
import { parseEnv } from "../env";
import { errorResult, guard, jsonResult, textResult } from "../result";

/**
 * The run-control tools: check whether an integration can start, run it (returning
 * a test URL), stop it, and read its logs. Each run is keyed by the caller's
 * per-session namespace (resolved from the MCP session id), so concurrent clients
 * drive independent runners. Run host I/O is injected as a {@link RunHostPort} so
 * these are testable without spawning a real `octo` process.
 */
export function registerRunTools(
  server: McpServer,
  config: OctoMcpConfig,
  runHost: RunHostPort,
  resolveNamespace: NamespaceResolver,
): void {
  const { store } = config;

  server.registerTool(
    "can_start_integration",
    {
      title: "Can start integration",
      description:
        "Check whether an integration is ready to run: whether a runner is available and whether its definition validates. Returns { available, valid, errors }.",
      inputSchema: { id: z.string().min(1).describe("The integration id.") },
    },
    ({ id }, extra) =>
      guard(async () => {
        const rec = await store.get(id);
        const { valid, errors } = config.validate(rec.definition);
        const ns = resolveNamespace(extra.sessionId);
        return jsonResult({
          available: runHost.status(ns).available,
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
          .record(z.string())
          .optional()
          .describe("Optional env vars injected into the run (name → value)."),
      },
    },
    ({ id, env }, extra) =>
      guard(async () => {
        const rec = await store.get(id);
        const ns = resolveNamespace(extra.sessionId);
        if (!runHost.status(ns).available) {
          return errorResult("Runner not available (OCTO_BIN_PATH unset).");
        }
        const { valid, errors } = config.validate(rec.definition);
        if (!valid) {
          return errorResult(`integration is not valid: ${errors.join("; ")}`);
        }
        let parsedEnv: Record<string, string> | undefined;
        if (env !== undefined) {
          const sane = parseEnv(env);
          if (!sane) return errorResult("invalid env (names must match [A-Za-z_][A-Za-z0-9_]* with string values)");
          parsedEnv = sane;
        }
        const st = await runHost.start(ns, rec.definition, parsedEnv);
        const testUrl = buildTestUrl(config, st);
        return jsonResult({
          running: st.running,
          exposable: st.exposable,
          testUrl,
          namespace: ns,
          note: st.exposable
            ? undefined
            : "Integration declares no HTTP_PORT, so it has no testable HTTP endpoint.",
        });
      }),
  );

  server.registerTool(
    "stop_integration",
    {
      title: "Stop integration",
      description:
        "Stop this session's running integration and free its resources. Returns { running: false }.",
      inputSchema: {},
    },
    (_args, extra) =>
      guard(async () => {
        const ns = resolveNamespace(extra.sessionId);
        const st = await runHost.stop(ns);
        return jsonResult({ running: st.running });
      }),
  );

  server.registerTool(
    "get_run_logs",
    {
      title: "Get run logs",
      description:
        "Read this session's runner log buffer (oldest line first), as plain text.",
      inputSchema: {},
    },
    (_args, extra) =>
      guard(async () => {
        const ns = resolveNamespace(extra.sessionId);
        const lines = runHost.snapshot(ns);
        if (lines.length === 0) {
          return textResult("(no logs yet — start a run with run_integration)");
        }
        return textResult(lines.map((l) => l.text).join("\n"));
      }),
  );
}

/** Absolutize a run's test path against `config.baseUrl`, or null when not networked. */
function buildTestUrl(config: OctoMcpConfig, st: RunStatusLike): string | null {
  if (!st.testPath) return null;
  const base = config.baseUrl?.replace(/\/+$/, "");
  return base ? `${base}${st.testPath}` : st.testPath;
}
