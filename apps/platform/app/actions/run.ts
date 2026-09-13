"use server";

/**
 * Server actions for the editor's RUN feature, which comes in two halves reached
 * quite differently:
 *
 *  - **The running app** (status/start/stop/sync) goes to a dev-run pod the orchestrator
 *    owns, through {@link remoteRunner}. Nothing about it lives in this process, since
 *    the platform runs several replicas with no session affinity.
 *  - **The one-shots** (invoke/evalCel/test) spawn a child of this very process from
 *    YAML carried in the request, and hold no state between calls.
 *
 * So a one-shot runs the buffer in front of the user, while the running app runs what
 * was SAVED, because its sidecar pulls the stored definition. `snapshotOf` reports
 * that to the editor as `reloadsOnSave`.
 *
 * status/sync require a session; start/stop require the write roles. The four
 * app-runner actions additionally resolve the caller's user id, since a dev run is
 * owned by (user, integration) and cannot be addressed without one.
 *
 * Every action leads with `tabId`, the browser half of the run namespace (see
 * `@/app/run/namespace`), rather than a field on the request objects, whose types
 * belong to @octo/editor and cannot know how a host keys its runners.
 */

import {
  binaries,
  evalCel,
  invoke,
  probeTestVersion,
  probeVersion,
  test,
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
import { UNSAVED, remoteRunner } from "@/app/run/remoteRunner";
import { withRead, withUser, withWrite, withWriteUser } from "./_auth";
import { resourcesFor, runKey, snapshotOf } from "./_run";

/** Whether RUN is available, whether a dev run is live for this integration, and versions. */
export async function runStatus(
  tabId: string,
  integrationId?: string,
): Promise<ActionResult<RunStatusSnapshot>> {
  return withUser(async (userId) => {
    // Warm both version caches so binaries() can read them synchronously. Two
    // binaries, two probes: dolphin can be absent while octo is present.
    await Promise.all([probeVersion(), probeTestVersion()]);
    try {
      const key = await runKey(tabId, userId, integrationId);
      return { ok: true, data: snapshotOf(await remoteRunner.status(key)) };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}

/**
 * Start a dev run for the open integration, or attach to the one already running it.
 *
 * `yaml` is accepted and ignored: the transport contract is shared with a host that
 * pushes config, while this one's sidecar pulls the saved definition. An unsaved draft
 * is therefore refused rather than started — there is nothing stored for it to run.
 */
export async function runStart(
  tabId: string,
  yaml: string,
  integrationId?: string,
): Promise<ActionResult<RunStatusSnapshot>> {
  return withWriteUser(async (userId) => {
    if (typeof integrationId !== "string" || integrationId.trim() === "") {
      return { ok: false, error: UNSAVED };
    }
    try {
      const key = await runKey(tabId, userId, integrationId);
      return { ok: true, data: snapshotOf(await remoteRunner.start(key, { yaml })) };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}

/** Tear the integration's dev run down. */
export async function runStop(
  tabId: string,
  integrationId?: string,
): Promise<ActionResult<RunStatusSnapshot>> {
  return withWriteUser(async (userId) => {
    try {
      const key = await runKey(tabId, userId, integrationId);
      return { ok: true, data: snapshotOf(await remoteRunner.stop(key)) };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}

/**
 * Evaluate a single CEL expression against an ad-hoc object (no flow run). Stateless
 * — it shells out to `octo eval` via @octo/run-host — but gated on the same runner
 * availability and read role as status/sync. CEL compile/eval failures come back as
 * `{ ok:false, error }` rather than thrown.
 */
export async function runEvalCel(
  tabId: string,
  req: CelEvalRequest,
): Promise<ActionResult<CelEvalResult>> {
  return withRead(async () => {
    // No namespace: `octo eval` spawns nothing that outlives the call and stages no
    // files, so there is nothing for one to scope.
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
 * quiet: LOG_LEVEL=error, so only real failures come back in `logs`.
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
        // Reduced to keys and type tags: the trace they are read from,
        // which holds the real bodies, never leaves the run host.
        learnShapes: req.learnShapes === true,
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
          ...(r.shapes ? { shapes: r.shapes } : {}),
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
 * the run covers the edit in front of the user. It spawns runners, so it takes the
 * write roles like invoke does.
 *
 * The outcome is copied field by field rather than spread: run-host's own result
 * carries dolphin's exit code, and the verdict is the tally.
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
        // Reduced to keys and type tags: the traces they are read from,
        // which hold the real bodies, never leave the run host.
        learnShapes: req.learnShapes === true,
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
  });
}

/**
 * Make the dev run pick up the integration's saved state now.
 *
 * Not the per-edit trigger: the reload that matters is fired by the orchestrator's own
 * write path, so it covers every writer rather than one editor, and `reloadsOnSave`
 * tells the editor to skip its debounced push. `yaml` is ignored here for the same
 * reason it is in {@link runStart}.
 */
export async function runSync(
  tabId: string,
  yaml: string,
  integrationId?: string,
): Promise<ActionResult<void>> {
  return withUser(async (userId) => {
    try {
      const key = await runKey(tabId, userId, integrationId);
      await remoteRunner.sync(key, { yaml });
      return { ok: true, data: undefined };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}
