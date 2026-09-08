import net from "node:net";

/**
 * Choosing the port the editor server listens on.
 *
 * This matters more than it looks. The server's URL is also its MCP endpoint, and
 * an agent configured against `http://127.0.0.1:8477/mcp` needs that to still be
 * true tomorrow. So the port is *deterministic first*: the same machine gets the
 * same port every launch unless something else has taken it. An ephemeral port
 * (`listen(0)`) would be simpler and would silently break every agent config on
 * every restart.
 *
 * 8477 avoids the two ranges this repo already spends: the run pools at
 * 40000-41999 (apps/standalone/app/run/ports.ts) and the runtime's observability
 * default at 39999. A run that collided with the editor would be a genuinely
 * confusing bug — the editor would go dark mid-run.
 */

export const PREFERRED_PORT = 8477;

/** How far to walk up before giving up on a deterministic port. */
export const FALLBACK_RANGE = 20;

/**
 * Whether a port can be bound on the loopback interface right now.
 *
 * A bind test rather than a connect test: connecting tells you whether something
 * is *answering*, which is a different question — a socket in TIME_WAIT answers
 * nothing and still refuses the bind.
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
 * it within the fallback range, else 0 — meaning "let the OS choose".
 *
 * The 0 case is the honest last resort. It gives up URL stability, so the caller
 * has to re-read the real port from the server and re-advertise it; what it does
 * not do is refuse to start, which would be the wrong trade for a user whose only
 * problem is that something else on their machine holds a range of ports.
 */
export async function choosePort(
  preferred = PREFERRED_PORT,
  range = FALLBACK_RANGE,
): Promise<number> {
  for (let port = preferred; port < preferred + range; port++) {
    if (await available(port)) return port;
  }
  return 0;
}

/**
 * The port a user pinned, if any. An escape hatch for the one situation the
 * fallback walk cannot fix: a machine where something already owns 8477 and the
 * user would rather move the editor than the other thing.
 */
export function pinnedPort(env: NodeJS.ProcessEnv = process.env): number | null {
  const raw = env.OCTO_DESKTOP_PORT;
  if (!raw) return null;
  const port = Number(raw);
  return Number.isInteger(port) && port > 0 && port < 65536 ? port : null;
}
