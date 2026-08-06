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
 * The platform's MCP run host.
 *
 * Still the in-process runner, and that is a known gap rather than a decision: the editor's
 * Run moved to a dev-run pod because a runner living in one replica's memory is invisible to
 * the others, and the run-control tools here have exactly the same problem. An agent that
 * calls `run_integration` and then `get_run_logs` can be answered by a different replica and
 * told there is nothing running.
 *
 * It stays for now because moving it is a change to the agent-facing contract, not just to
 * the wiring: a dev run is addressed by (user, integration), so `stop_integration` and
 * `get_run_logs` — which today mean "this MCP session's run" and take no arguments — have to
 * name the integration. That is its own commit. This module is where it lands, which is why
 * it exists at all rather than the handler defaulting to the same module.
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
