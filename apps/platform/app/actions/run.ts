"use server";

/**
 * Server actions for the editor's RUN feature. Unlike the orchestrator actions,
 * these don't make HTTP calls — they drive the in-process @octo/run-host directly,
 * keyed by the calling tab's run namespace. The live log stream stays an SSE route
 * (`/api/run/logs`), which resolves the same namespace from the same two halves.
 * status/sync require a session; start/stop require the write roles — matching the
 * route handlers they replace.
 *
 * Every action leads with `tabId`, the browser half of that namespace (see
 * `@/app/run/namespace`). It is a separate parameter rather than a field on the
 * request objects because those types belong to @octo/editor, which has no business
 * knowing how a host keys its runners.
 */

import {
  binaries,
  evalCel,
  invoke,
  probeTestVersion,
  probeVersion,
  start,
  status,
  stop,
  sync,
  test,
  type RunState,
} from "@octo/run-host";
import type {
  CelEvalRequest,
  CelEvalResult,
  FlowRunOutcome,
  FlowRunRequest,
  RunStatusSnapshot,
  TestRunOutcome,
  TestRunRequest,
} from "@octo/editor";
import type { ActionResult } from "@octo/http";
import { ensureRunNamespace } from "@/app/run/namespace";
import { orchestratorResourceProvider } from "@/app/lib/runResources";
import { withRead, withWrite } from "./_auth";

/**
 * The editor snapshot, composed from the two things it asks about at once: what this
 * host can spawn (its binaries, which back the one-shot debug runs) and what the app
 * runner is currently doing. They are separate because they answer to different owners
 * — a binary is installed on the host, a run belongs to a backend.
 */
function snapshotOf(state: RunState): RunStatusSnapshot {
  return { ...binaries(), ...state };
}

/** The resource provider for a run, or undefined for an unsaved draft (no id). */
function resourcesFor(integrationId?: unknown) {
  if (typeof integrationId !== "string" || integrationId.trim() === "") return undefined;
  return orchestratorResourceProvider(integrationId);
}

/** Whether RUN is available, whether this browser's runner is live, and its version. */
export async function runStatus(tabId: string): Promise<ActionResult<RunStatusSnapshot>> {
  return withRead(async () => {
    // Warm both version caches so status() can read them synchronously. Two
    // binaries, two probes: dolphin can be absent while octo is present.
    await Promise.all([probeVersion(), probeTestVersion()]);
    const ns = await ensureRunNamespace(tabId);
    return { ok: true, data: snapshotOf(status(ns)) };
  });
}

/** Render the config and (re)start this browser's runner. */
export async function runStart(
  tabId: string,
  yaml: string,
  integrationId?: string,
): Promise<ActionResult<RunStatusSnapshot>> {
  return withWrite(async () => {
    const ns = await ensureRunNamespace(tabId);
    if (!binaries().available) {
      return { ok: false, error: "Runner not available (OCTO_BIN_PATH unset)." };
    }
    if (typeof yaml !== "string" || yaml.trim() === "") {
      return { ok: false, error: "missing `yaml`" };
    }
    try {
      return {
        ok: true,
        data: snapshotOf(
          await start(ns, yaml, undefined, { resources: resourcesFor(integrationId) }),
        ),
      };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}

/** Stop this browser's runner and clean up its config file. */
export async function runStop(tabId: string): Promise<ActionResult<RunStatusSnapshot>> {
  return withWrite(async () => {
    const ns = await ensureRunNamespace(tabId);
    return { ok: true, data: snapshotOf(await stop(ns)) };
  });
}

/**
 * Evaluate a single CEL expression against an ad-hoc object (no flow run). Stateless
 * — it shells out to `octo eval` via @octo/run-host — but gated on the same runner
 * availability and read role as status/sync. CEL compile/eval failures come back as
 * `{ ok:false, error }`, not thrown.
 */
export async function runEvalCel(
  tabId: string,
  req: CelEvalRequest,
): Promise<ActionResult<CelEvalResult>> {
  return withRead(async () => {
    const ns = await ensureRunNamespace(tabId);
    if (!binaries().available) {
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
  tabId: string,
  req: FlowRunRequest,
): Promise<ActionResult<FlowRunOutcome>> {
  return withWrite(async () => {
    const ns = await ensureRunNamespace(tabId);
    if (!binaries().available) {
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
        spies: req.spies,
        mocks: req.mocks,
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
          spies: r.spies,
        },
      };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}

/**
 * Run a flow's dolphin suites — the Testing tab's Run.
 *
 * The suites travel in the request rather than being read from the resource store, so
 * the tab runs the edit in front of the user; `runInvoke` takes `yaml` for the same
 * reason. It spawns runners, so it takes the write roles like invoke does.
 *
 * The outcome is copied field by field rather than spread. run-host's own result carries
 * dolphin's exit code, which is a detail of how the run was made and not something the
 * UI should reason about — the verdict is the tally.
 */
export async function runTest(
  tabId: string,
  req: TestRunRequest,
): Promise<ActionResult<TestRunOutcome>> {
  return withWrite(async () => {
    const ns = await ensureRunNamespace(tabId);
    if (!binaries().testAvailable) {
      return { ok: false, error: "Test runner not available (DOLPHIN_BIN_PATH unset)." };
    }
    if (typeof req?.yaml !== "string" || req.yaml.trim() === "") {
      return { ok: false, error: "missing `yaml`" };
    }
    if (!Array.isArray(req?.suites) || req.suites.length === 0) {
      return { ok: false, error: "no test suites to run" };
    }
    try {
      const r = await test(ns, {
        yaml: req.yaml,
        suites: req.suites,
        env: req.env,
        resources: resourcesFor(req.integrationId),
      });
      return {
        ok: true,
        data: {
          ok: r.ok,
          timedOut: r.timedOut,
          totals: r.totals,
          suites: r.suites,
          logs: r.logs,
          ...(r.error !== undefined ? { error: r.error } : {}),
        },
      };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}

/** Rewrite this browser's watched config so the runner hot-reloads. */
export async function runSync(
  tabId: string,
  yaml: string,
  integrationId?: string,
): Promise<ActionResult<void>> {
  return withRead(async () => {
    const ns = await ensureRunNamespace(tabId);
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
