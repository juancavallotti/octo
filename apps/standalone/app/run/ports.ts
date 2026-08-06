import YAML from "yaml";

/**
 * Port allocation for namespaced editor runs. Two things a run needs a real,
 * unique port for:
 *
 * - **HTTP.** A networked integration (one that declares HTTP_PORT) needs a listen
 *   port this app can proxy to; with many concurrent users we hand each run one from
 *   a pool starting at 40000 and inject it as HTTP_PORT when spawning `octo` —
 *   mirroring how the orchestrator overrides the declared port in production.
 * - **The admin port.** The runtime's observability service (probes and metrics)
 *   is on by default and binds a fixed `:39999`, which every run would fight over:
 *   in production one run owns its pod, but here they share a host. So each run
 *   also gets an admin port, injected as OCTO_OBSERVABILITY_ADDR, and several runs
 *   can be monitored at once instead of the second one starting without probes.
 *
 * The two live in separate ranges rather than one pool, so a run's admin port can
 * never be handed to another run's HTTP listener (and a port in a log line says
 * which of the two it is).
 */

/**
 * First HTTP port handed out. The pool is local to this process, which is what confines it
 * to this app: a second replica would hand out the same numbers for different runs and know
 * nothing of the first's. That is the property that made a shared local runner untenable
 * on the platform, and it is stated here because this is where it is decided.
 */
const BASE_PORT = 40000;
/** Inclusive top of the HTTP pool — 1000 concurrent networked runs per editor pod. */
const MAX_PORT = 40999;

/** First admin port handed out; one per run, networked or not. */
const ADMIN_BASE_PORT = 41000;
/** Inclusive top of the admin pool — 1000 concurrent runs per editor pod. */
const ADMIN_MAX_PORT = 41999;

const store = globalThis as unknown as {
  __octoRunPorts?: Set<number>;
  __octoRunAdminPorts?: Set<number>;
};

/** One allocation range: its bounds, what it is for, and the set of ports it has
 * handed out. State lives on `globalThis` so it survives Next's dev HMR reloads,
 * like the session map it belongs to. */
interface Pool {
  base: number;
  max: number;
  /** Names the pool in the exhaustion error. */
  what: string;
  used(): Set<number>;
}

const runPool: Pool = {
  base: BASE_PORT,
  max: MAX_PORT,
  what: "run",
  used() {
    if (!store.__octoRunPorts) store.__octoRunPorts = new Set();
    return store.__octoRunPorts;
  },
};

const adminPool: Pool = {
  base: ADMIN_BASE_PORT,
  max: ADMIN_MAX_PORT,
  what: "admin",
  used() {
    if (!store.__octoRunAdminPorts) store.__octoRunAdminPorts = new Set();
    return store.__octoRunAdminPorts;
  },
};

/** Reserve and return the lowest free port in a pool. Throws when it is exhausted
 * (surfaced to the caller as a start failure). */
function allocate(pool: Pool): number {
  const used = pool.used();
  for (let p = pool.base; p <= pool.max; p++) {
    if (!used.has(p)) {
      used.add(p);
      return p;
    }
  }
  throw new Error(
    `no free ${pool.what} port available (pool ${pool.base}-${pool.max} exhausted)`,
  );
}

/** allocatePort reserves and returns the lowest free HTTP port. */
export function allocatePort(): number {
  return allocate(runPool);
}

/** releasePort returns an HTTP port to the pool. Idempotent. */
export function releasePort(port: number): void {
  runPool.used().delete(port);
}

/** allocateAdminPort reserves and returns the lowest free admin (observability) port. */
export function allocateAdminPort(): number {
  return allocate(adminPool);
}

/** releaseAdminPort returns an admin port to the pool. Idempotent. */
export function releaseAdminPort(port: number): void {
  adminPool.used().delete(port);
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
