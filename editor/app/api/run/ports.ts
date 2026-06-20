import YAML from "yaml";

/**
 * HTTP port allocation for namespaced editor runs. A networked integration (one
 * that declares HTTP_PORT) needs a real, unique listen port so the BFF can proxy
 * to it; with many concurrent users we hand each run a port from a dedicated pool
 * starting at 40000 and inject it as HTTP_PORT when spawning `octo` — mirroring how
 * the orchestrator overrides the declared port in production.
 */

/** First port handed out; the pool is editor-pod-local so this fixed range is safe. */
const BASE_PORT = 40000;
/** Inclusive top of the pool — 1000 concurrent networked runs per editor pod. */
const MAX_PORT = 40999;

const store = globalThis as unknown as {
  __octoRunPorts?: Set<number>;
};

function inUse(): Set<number> {
  if (!store.__octoRunPorts) store.__octoRunPorts = new Set();
  return store.__octoRunPorts;
}

/** allocatePort reserves and returns the lowest free port in the pool. Throws when
 * the pool is exhausted (surfaced to the caller as a start failure). */
export function allocatePort(): number {
  const used = inUse();
  for (let p = BASE_PORT; p <= MAX_PORT; p++) {
    if (!used.has(p)) {
      used.add(p);
      return p;
    }
  }
  throw new Error(`no free run port available (pool ${BASE_PORT}-${MAX_PORT} exhausted)`);
}

/** releasePort returns a port to the pool. Idempotent. */
export function releasePort(port: number): void {
  inUse().delete(port);
}

/** envHTTPPort is the variable an integration declares to bind an HTTP listener;
 * declaring it (with a numeric default) is what makes a run networked/exposable. */
const envHTTPPort = "HTTP_PORT";

interface envDecl {
  env?: Array<{ name?: string; default?: unknown }>;
}

/**
 * isExposable reports whether the rendered run YAML declares HTTP_PORT with a
 * usable numeric default (1-65535) — the same rule the orchestrator applies in
 * production (see orchestrator resolveRuntimeEnv). A malformed document is treated
 * as internal-only; the runtime validates the full document at load time.
 */
export function isExposable(yaml: string): boolean {
  let decl: envDecl;
  try {
    decl = (YAML.parse(yaml) ?? {}) as envDecl;
  } catch {
    return false;
  }
  for (const e of decl.env ?? []) {
    if (e?.name?.trim() !== envHTTPPort) continue;
    const raw = e.default;
    const port = typeof raw === "number" ? raw : parseInt(String(raw).trim(), 10);
    return Number.isInteger(port) && port > 0 && port <= 65535;
  }
  return false;
}
