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
 * it within the fallback range. Throws when the whole range is taken.
 *
 * It used to return 0 — "let the OS choose" — which read as the gracious option
 * and was in fact broken: nothing ever asked the child which port it had actually
 * bound, so the app built `http://127.0.0.1:0`, polled a port nobody listens on,
 * and put the user through the full 30s readiness timeout before failing. An error
 * naming the exhausted range is both honest and more useful; recovering properly
 * would mean parsing the bound port back out of the child, which is a real feature
 * and not a fallback.
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
