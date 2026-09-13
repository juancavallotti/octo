import { binaries, evalCel, invoke, newNamespace, test } from "@octo/run-host";
import type { RunHostPort } from "@octo/mcp";
import { logTail, remoteRunner } from "@/app/run/remoteRunner";

/**
 * The platform's MCP run host, split two ways.
 *
 * The long-running app (`run_integration`, `stop_integration`, `get_run_logs`) is a dev-run
 * pod the orchestrator owns, so an agent that starts a run on one replica can read its logs
 * from another. The one-shots (`invoke_flow`, `evaluate_cel`, `run_tests`) spawn a child of
 * this pod from YAML in the request and finish inside the call.
 *
 * An agent and a human editing the same integration share one run: both derive the same
 * identity from (user, integration).
 */

/**
 * Why an ad-hoc `env` cannot be honoured here — a known gap, tracked as
 * https://github.com/juancavallotti/octo/issues/239. Refused rather than dropped in the
 * meantime, because a value that vanishes silently sends the caller to debug the flow
 * instead of the call.
 */
export const ENV_UNSUPPORTED =
  "A dev run reads its environment from the integration's stored resources, not from the " +
  "request, so `env` cannot be injected per run. Put the values in the integration's " +
  '`.env.dev` resource (see `update_resource`) and run it again. `invoke_flow`, ' +
  "`evaluate_cel` and `run_tests` still take `env` — they run locally.";

export const devRunMcpHost: RunHostPort = {
  // These describe the binaries backing the one-shots below, which run here whatever the
  // long-running app does.
  binaries,

  /**
   * `yaml` is passed through and ignored — the run's sidecar pulls the saved definition —
   * so an unsaved change is not what runs. `env` is refused rather than dropped: an agent
   * that set a value and got a run without it would debug the flow instead of the call.
   */
  // Async even though the refusal is synchronous: a function the port says returns a
  // promise must reject rather than throw, or a caller that only guards the promise misses
  // it.
  start: async (key, yaml, env) => {
    if (env && Object.keys(env).length > 0) throw new Error(ENV_UNSUPPORTED);
    // No resource provider either: the sidecar fetches the integration's resources
    // itself, so staging them here would be work nothing reads.
    return remoteRunner.start(key, { yaml });
  },

  stop: (key) => remoteRunner.stop(key),

  snapshot: (key) => logTail(key),

  invoke,
  evalCel,
  test,
  newNamespace,
};
