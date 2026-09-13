import { binaries, evalCel, invoke, newNamespace, test } from "@octo/run-host";
import type { RunHostPort } from "@octo/mcp";
import { localRunner } from "@/app/run/localRunner";
import { snapshot } from "@/app/run/session";

/**
 * This app's MCP run host: one process, one machine, the `octo` binary beside it, so
 * every operation is a local one. It is declared rather than defaulted to, so nothing
 * can silently fall back to a runner in the wrong place.
 *
 * Of the run key, only the namespace is read: a run here is a child of this process
 * keyed by the MCP session, and nothing outlives the machine.
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
