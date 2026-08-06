"use server";

/**
 * Server actions for the editor's RUN feature (standalone). They drive the
 * in-process @octo/run-host directly (no HTTP, no auth — local-only), keyed by the
 * calling tab's run namespace. The live log stream stays an SSE route
 * (`/api/run/logs`), which resolves the same namespace from the same two halves.
 *
 * Every action leads with `tabId`, the browser half of that namespace (see
 * `../run/namespace`). It is a separate parameter rather than a field on the
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
import { ensureRunNamespace } from "../run/namespace";
import { fsResourceProvider } from "../run/resources";

/**
 * The editor's snapshot, composed from the two things it asks about at once: what this
 * host can spawn (its binaries) and what the app runner is currently doing.
 *
 * They are separate because they answer to different owners — a binary is installed on
 * the host, a run belongs to a backend — and here they happen to be the same process.
 */
function snapshotOf(state: RunState): RunStatusSnapshot {
  return { ...binaries(), ...state };
}

/** Whether RUN is available, whether this browser's runner is live, and its version. */
export async function runStatus(tabId: string): Promise<ActionResult<RunStatusSnapshot>> {
  // Warm both version caches so binaries() can read them synchronously. Two
  // binaries, two probes: dolphin can be absent while octo is present.
  await Promise.all([probeVersion(), probeTestVersion()]);
  const ns = await ensureRunNamespace(tabId);
  return { ok: true, data: snapshotOf(status(ns)) };
}

/** Render the config and (re)start this browser's runner. */
export async function runStart(
  tabId: string,
  yaml: string,
): Promise<ActionResult<RunStatusSnapshot>> {
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
      data: snapshotOf(await start(ns, yaml, undefined, { resources: fsResourceProvider })),
    };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/** Stop this browser's runner and clean up its config file. */
export async function runStop(tabId: string): Promise<ActionResult<RunStatusSnapshot>> {
  const ns = await ensureRunNamespace(tabId);
  return { ok: true, data: snapshotOf(await stop(ns)) };
}

/** Evaluate a single CEL expression against an ad-hoc object (no flow run). */
export async function runEvalCel(
  tabId: string,
  req: CelEvalRequest,
): Promise<ActionResult<CelEvalResult>> {
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
}

/**
 * Run one flow once and return what it produced, without starting the integration's
 * sources — the editor's debug path. A finished run reports its result message
 * (`{event_id, variables, body}`); `breakAt` halts the flow at a block and reports the
 * message as it looked there instead.
 *
 * Manual runs are quiet: LOG_LEVEL=error, so the runner's own startup chatter stays
 * out of the way and only real failures come back in `logs` (which the Problems tab
 * shows). That is a policy of the host, not of the caller.
 */
export async function runInvoke(
  tabId: string,
  req: FlowRunRequest,
): Promise<ActionResult<FlowRunOutcome>> {
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
      resources: fsResourceProvider,
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
}

/**
 * Run a flow's dolphin suites — the Testing tab's Run.
 *
 * The suites come in the request rather than being read off disk, so the tab runs the
 * edit in front of the user; `runInvoke` takes `yaml` for the same reason.
 *
 * The outcome is copied field by field rather than spread. run-host's own result carries
 * dolphin's exit code, which is a detail of how the run was made and not something the
 * UI should reason about — the verdict is the tally.
 */
export async function runTest(
  tabId: string,
  req: TestRunRequest,
): Promise<ActionResult<TestRunOutcome>> {
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
      resources: fsResourceProvider,
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
}

/** Rewrite this browser's watched config so the runner hot-reloads. */
export async function runSync(tabId: string, yaml: string): Promise<ActionResult<void>> {
  const ns = await ensureRunNamespace(tabId);
  if (typeof yaml !== "string" || yaml.trim() === "") {
    return { ok: false, error: "missing `yaml`" };
  }
  try {
    await sync(ns, yaml, { resources: fsResourceProvider });
    return { ok: true, data: undefined };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}
