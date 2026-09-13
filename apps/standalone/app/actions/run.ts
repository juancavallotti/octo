"use server";

/**
 * Server actions for the editor's RUN feature. They drive this app's own runner
 * directly — no HTTP, no auth — keyed by the calling tab's run namespace, which the
 * SSE log route resolves the same way.
 *
 * The long-running app comes from `../run/localRunner`, which spawns `octo run --watch`
 * as a child of this process; the one-shots and binary probes come from @octo/run-host.
 *
 * Every action leads with `tabId`, the browser half of that namespace (see
 * `../run/namespace`), as a parameter rather than a field on the request objects: those
 * types know nothing about how a host keys its runners.
 */

import {
  binaries,
  evalCel,
  invoke,
  probeTestVersion,
  probeVersion,
  test,
  type RunState,
} from "@octo/run-host";
import { start, stop, sync } from "../run/localRunner";
import { status } from "../run/session";
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
  // No namespace: `octo eval` reads an expression and an object, spawns nothing that
  // outlives the call, and stages no files — so there is nothing for one to scope.
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
 * Manual runs are quiet — LOG_LEVEL=error — so only real failures come back in
 * `logs`. That is this host's policy, not the caller's.
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
      // The shapes come back reduced to keys and type tags — the trace they are read
      // from never leaves the run host, which is where the real bodies are.
      learnShapes: req.learnShapes === true,
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
        ...(r.shapes ? { shapes: r.shapes } : {}),
      },
    };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/**
 * Run a flow's dolphin suites.
 *
 * The suites come in the request rather than off disk, so what runs is the unsaved
 * edit; `runInvoke` takes `yaml` for the same reason. The outcome is copied field by
 * field rather than spread, so dolphin's exit code stays out of the answer: the
 * verdict is the tally.
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
      // The shapes come back reduced to keys and type tags — the traces they are read
      // from never leave the run host, which is where the real bodies are.
      learnShapes: req.learnShapes === true,
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
        ...(r.shapes ? { shapes: r.shapes } : {}),
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
