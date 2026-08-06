"use server";

/**
 * Server actions for the editor's RUN feature — and the platform's RUN feature comes in
 * two halves that are reached quite differently:
 *
 *  - **The running app** (status/start/stop/sync) goes to a dev-run pod the orchestrator
 *    owns, through {@link remoteRunner}. Nothing about it lives in this process, which is
 *    the point: the platform runs several replicas with no session affinity, and the
 *    in-process runner it used to drive was invisible to every replica but the one that
 *    started it.
 *  - **The one-shots** (invoke/evalCel/test) still spawn a child of this very process from
 *    YAML carried in the request. They hold no state between calls, so fan-out never broke
 *    them and there is nothing to move.
 *
 * The split shows up in what the two halves run: a one-shot runs the buffer in front of
 * the user, while the running app runs what was SAVED, because its sidecar pulls the
 * stored definition. `snapshotOf` reports that to the editor as `reloadsOnSave`.
 *
 * status/sync require a session; start/stop require the write roles — matching the route
 * handlers they replace. The four app-runner actions additionally resolve the caller's
 * user id, since a dev run is owned by (user, integration) and cannot be addressed
 * without one.
 *
 * Every action leads with `tabId`, the browser half of the run namespace (see
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
  test,
  type RunKey,
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
import { UNSAVED, remoteRunner } from "@/app/run/remoteRunner";
import { withRead, withUser, withWrite, withWriteUser } from "./_auth";

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

/**
 * The key the app runner addresses: the owning user and integration, which is what a dev
 * run *is*, plus the run namespace, which it ignores.
 *
 * The namespace is resolved anyway rather than skipped, and not out of politeness to the
 * type: `ensureRunNamespace` is what mints the run cookie, and the one-shots below key
 * their staging directories on it. Dropping it here would leave the first `invoke` of a
 * session to mint it instead — which works, and hides why it has to happen at all.
 */
async function runKey(
  tabId: string,
  userId: string,
  integrationId?: string,
): Promise<RunKey> {
  return { namespace: await ensureRunNamespace(tabId), userId, integrationId };
}

/** Whether RUN is available, whether a dev run is live for this integration, and versions. */
export async function runStatus(
  tabId: string,
  integrationId?: string,
): Promise<ActionResult<RunStatusSnapshot>> {
  return withUser(async (userId) => {
    // Warm both version caches so binaries() can read them synchronously. Two
    // binaries, two probes: dolphin can be absent while octo is present. Still local, and
    // still worth reporting — the debug features they back work whatever the dev run does.
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
 * `yaml` is accepted and ignored: the editor sends its buffer because the transport
 * contract is shared with a host that pushes config, and this one's sidecar pulls the
 * saved definition instead. So an unsaved draft is refused rather than started — there is
 * nothing stored for it to run, and starting something that ran stale YAML would be worse
 * than saying so.
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
 * `{ ok:false, error }`, not thrown.
 */
export async function runEvalCel(
  tabId: string,
  req: CelEvalRequest,
): Promise<ActionResult<CelEvalResult>> {
  return withRead(async () => {
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

/**
 * Make the dev run pick up the integration's saved state now.
 *
 * Not the per-edit trigger, and the editor knows not to use it as one: `reloadsOnSave`
 * tells it to skip the debounced push entirely, because the reload that matters is fired
 * by the orchestrator's own write path — which is what makes it cover MCP and API-key
 * writers too, not just this editor. `yaml` is ignored here for the same reason it is in
 * {@link runStart}.
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
