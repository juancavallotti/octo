import {
  binaries,
  evalCel,
  invoke,
  newNamespace,
  snapshot,
  start,
  stop,
  test,
} from "@octo/run-host";
import type { RunHostPort } from "@octo/mcp";

/**
 * The standalone app's MCP run host: the in-process runner, which here is simply the
 * runner. This app IS the host — one process, one machine, the `octo` binary beside it —
 * so every operation is a local one and the port is a thin naming of what
 * `@octo/run-host` already exports.
 *
 * It is declared rather than defaulted to, and that is the point of the seam: the platform
 * runs an integration somewhere else entirely, and a port that fell back to this module
 * would hand it a runner in the wrong pod with nothing to say so.
 */
export const localMcpRunHost: RunHostPort = {
  binaries,
  start,
  stop,
  invoke,
  evalCel,
  test,
  // Wrapped rather than passed: the local buffer answers synchronously, while the port is
  // asynchronous so that a host reading logs over the network can satisfy it too.
  snapshot: async (ns) => snapshot(ns),
  newNamespace,
};
