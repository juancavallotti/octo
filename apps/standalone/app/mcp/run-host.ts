import { binaries, evalCel, invoke, newNamespace, test } from "@octo/run-host";
import type { RunHostPort } from "@octo/mcp";
import { localRunner } from "@/app/run/localRunner";
import { snapshot } from "@/app/run/session";

/**
 * The standalone app's MCP run host: the in-process runner, which here is simply the
 * runner. This app IS the host — one process, one machine, the `octo` binary beside it —
 * so every operation is a local one.
 *
 * It is declared rather than defaulted to, and that is the point of the seam: the platform
 * runs an integration in a pod of its own, and a port that fell back to this module would
 * hand it a runner in the wrong place with nothing to say so.
 *
 * Of the run key, only the namespace is read. A local run is a child of this process keyed
 * by the MCP session, so the integration and the caller name nothing here — there is no
 * second user to be separated from, and nothing outlives the machine.
 */
export const localMcpRunHost: RunHostPort = {
  binaries,

  start: (key, yaml, devEnv, opts) =>
    localRunner.start(key, { yaml, devEnv, resources: opts?.resources }),

  stop: (key) => localRunner.stop(key),

  // Wrapped rather than passed straight through: the local buffer answers synchronously,
  // while the port is asynchronous so a host reading logs over the network can satisfy it.
  snapshot: async (key) => snapshot(key.namespace),

  invoke,
  evalCel,
  test,
  newNamespace,
};
