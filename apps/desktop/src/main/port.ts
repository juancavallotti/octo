import net from "node:net";

/**
 * The editor server's URL is also its MCP endpoint, so the port is deterministic:
 * the same machine gets the same port every launch unless something else has taken
 * it, and an agent configured against `http://127.0.0.1:8477/mcp` still reaches it
 * tomorrow. 8477 stays clear of the port ranges runs and observability already use.
 */

export const PREFERRED_PORT = 8477;

/** How far to walk up before giving up on a deterministic port. */
export const FALLBACK_RANGE = 20;

/**
 * Whether a port can be bound on the loopback interface right now.
 *
 * A bind test rather than a connect test: a socket in TIME_WAIT answers nothing
 * and still refuses the bind.
 */
export function available(port: number, host = "127.0.0.1"): Promise<boolean> {
  return new Promise((resolve) => {
    const probe = net.createServer();
    probe.once("error", () => resolve(false));
    probe.once("listening", () => probe.close(() => resolve(true)));
    probe.listen(port, host);
  });
}

/**
 * The port to serve on: `preferred` if it is free, else the first free port above
 * it within the fallback range. Throws when the whole range is taken, naming the
 * range: nothing reads the port back out of the child, so an ephemeral port would
 * leave callers polling an address nobody listens on.
 */
export async function choosePort(
  preferred = PREFERRED_PORT,
  range = FALLBACK_RANGE,
): Promise<number> {
  for (let port = preferred; port < preferred + range; port++) {
    if (await available(port)) return port;
  }
  throw new Error(
    `No free port between ${preferred} and ${preferred + range - 1}. ` +
      "Close whatever is using them, or pin a different one with OCTO_DESKTOP_PORT.",
  );
}

/**
 * The port a user pinned in `OCTO_DESKTOP_PORT`, if it parses as a port; null
 * otherwise.
 */
export function pinnedPort(env: NodeJS.ProcessEnv = process.env): number | null {
  const raw = env.OCTO_DESKTOP_PORT;
  if (!raw) return null;
  const port = Number(raw);
  return Number.isInteger(port) && port > 0 && port < 65536 ? port : null;
}
