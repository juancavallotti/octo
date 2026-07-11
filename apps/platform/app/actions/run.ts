"use server";

/**
 * Server actions for the editor's RUN feature. Unlike the orchestrator actions,
 * these don't make HTTP calls — they drive the in-process @octo/run-host directly,
 * keyed by this browser's run namespace (a cookie, minted here when absent). The
 * live log stream stays an SSE route (`/api/run/logs`), which reads the same
 * cookie. status/sync require a session; start/stop require the write roles —
 * matching the route handlers they replace.
 */

import { evalCel, invoke, probeVersion, start, status, stop, sync } from "@octo/run-host";
import type {
  CelEvalRequest,
  CelEvalResult,
  FlowRunOutcome,
  FlowRunRequest,
  RunStatusSnapshot,
} from "@octo/editor";
import type { ActionResult } from "@octo/http";
import { ensureRunNamespace } from "@/app/run/namespace";
import { orchestratorResourceProvider } from "@/app/lib/runResources";
import { withRead, withWrite } from "./_auth";

/** The resource provider for a run, or undefined for an unsaved draft (no id). */
function resourcesFor(integrationId?: unknown) {
  if (typeof integrationId !== "string" || integrationId.trim() === "") return undefined;
  return orchestratorResourceProvider(integrationId);
}

/** Whether RUN is available, whether this browser's runner is live, and its version. */
export async function runStatus(): Promise<ActionResult<RunStatusSnapshot>> {
  return withRead(async () => {
    await probeVersion(); // warm the version cache so status() can read it
    const ns = await ensureRunNamespace();
    return { ok: true, data: status(ns) };
  });
}

/** Render the config and (re)start this browser's runner. */
export async function runStart(
  yaml: string,
  integrationId?: string,
): Promise<ActionResult<RunStatusSnapshot>> {
  return withWrite(async () => {
    const ns = await ensureRunNamespace();
    if (!status(ns).available) {
      return { ok: false, error: "Runner not available (OCTO_BIN_PATH unset)." };
    }
    if (typeof yaml !== "string" || yaml.trim() === "") {
      return { ok: false, error: "missing `yaml`" };
    }
    try {
      return {
        ok: true,
        data: await start(ns, yaml, undefined, {
          resources: resourcesFor(integrationId),
        }),
      };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}

/** Stop this browser's runner and clean up its config file. */
export async function runStop(): Promise<ActionResult<RunStatusSnapshot>> {
  return withWrite(async () => {
    const ns = await ensureRunNamespace();
    return { ok: true, data: await stop(ns) };
  });
}

/**
 * Evaluate a single CEL expression against an ad-hoc object (no flow run). Stateless
 * — it shells out to `octo eval` via @octo/run-host — but gated on the same runner
 * availability and read role as status/sync. CEL compile/eval failures come back as
 * `{ ok:false, error }`, not thrown.
 */
export async function runEvalCel(
  req: CelEvalRequest,
): Promise<ActionResult<CelEvalResult>> {
  return withRead(async () => {
    const ns = await ensureRunNamespace();
    if (!status(ns).available) {
      return { ok: false, error: "Runner not available (OCTO_BIN_PATH unset)." };
    }
    if (typeof req?.expression !== "string" || req.expression.trim() === "") {
      return { ok: false, error: "missing `expression`" };
    }
    try {
      const r = await evalCel(req.expression, {
        data: req.data,
        vars: req.vars,
        env: req.env,
      });
      return { ok: true, data: { ok: r.ok, result: r.result, error: r.error } };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}

/**
 * Run one flow once and return what it produced, without starting the integration's
 * sources — the editor's debug path. A finished run reports its result message
 * (`{event_id, variables, body}`); `breakAt` halts the flow at a block and reports the
 * message as it looked there instead.
 *
 * It spawns a runner, so it takes the write roles, like start/stop. Manual runs are
 * quiet: LOG_LEVEL=error, so the runner's startup chatter stays out of the way and
 * only real failures come back in `logs`. That is the host's policy, not the caller's.
 */
export async function runInvoke(
  req: FlowRunRequest,
): Promise<ActionResult<FlowRunOutcome>> {
  return withWrite(async () => {
    const ns = await ensureRunNamespace();
    if (!status(ns).available) {
      return { ok: false, error: "Runner not available (OCTO_BIN_PATH unset)." };
    }
    if (typeof req?.yaml !== "string" || req.yaml.trim() === "") {
      return { ok: false, error: "missing `yaml`" };
    }
    if (typeof req?.flow !== "string" || req.flow.trim() === "") {
      return { ok: false, error: "missing `flow`" };
    }
    try {
      const r = await invoke(ns, req.yaml, req.flow, {
        data: req.data,
        vars: req.vars,
        breakAt: req.breakAt,
        logLevel: "error",
        resources: resourcesFor(req.integrationId),
      });
      return {
        ok: true,
        data: {
          ok: r.ok,
          dropped: r.dropped,
          timedOut: r.timedOut,
          output: r.output,
          logs: r.logs,
          breakpoint: r.breakpoint,
        },
      };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}

/** Rewrite this browser's watched config so the runner hot-reloads. */
export async function runSync(
  yaml: string,
  integrationId?: string,
): Promise<ActionResult<void>> {
  return withRead(async () => {
    const ns = await ensureRunNamespace();
    if (typeof yaml !== "string" || yaml.trim() === "") {
      return { ok: false, error: "missing `yaml`" };
    }
    try {
      await sync(ns, yaml, { resources: resourcesFor(integrationId) });
      return { ok: true, data: undefined };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}
