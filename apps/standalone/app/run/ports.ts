import YAML from "yaml";

/**
 * Port allocation for namespaced editor runs. Two things a run needs a real,
 * unique port for:
 *
 * - **HTTP.** A networked integration (one that serves an HTTP source) needs a listen
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

/** The env vars an HTTP listener binds to. This host supplies both when it starts a
 * run — the port from its pool, the loopback host only the same-process proxy needs —
 * so what matters about a document is whether its listener will take them. */
const envHTTPPort = "HTTP_PORT";
const envHTTPHost = "HTTP_HOST";

/**
 * httpConnectorType is the runtime type of the connector that owns an HTTP listener —
 * both a connector's declared `type` and the `type` of the sources it exposes, which
 * is what lets a source name it where an instance name goes.
 */
const httpConnectorType = "http";

/** The bind-all address. A connector that pins it is pinning what would have been
 * supplied anyway, so it stays reachable. */
const bindAllHost = "0.0.0.0";

/** The slice of a run document this file reads: what it declares as environment, what
 * connectors it configures, and what its flows bind their sources to. */
interface runDecl {
  env?: Array<{ name?: string; default?: unknown }>;
  connectors?: Array<{
    name?: string;
    type?: string;
    settings?: { host?: unknown; port?: unknown };
  }>;
  flows?: Array<{ source?: { connector?: string; type?: string } }>;
}

/**
 * isExposable reports whether a run serves HTTP on the port this host hands it — the
 * same question the orchestrator's resolveRuntimeEnv answers in production, and it
 * must stay the same question: the two disagreeing means the editor promises a test
 * URL the platform will not publish, or the reverse.
 *
 * It is NOT "does the document declare HTTP_PORT". This host does not read the port so
 * much as choose it: it allocates one from a pool, injects it, and proxies to what it
 * injected. So the only thing that matters is whether the listener takes that value —
 * see {@link wiresInjectedPort}, which is also where every way of failing to is
 * written down. A declaration nothing reads is not an endpoint, and an HTTP source
 * with no declaration at all is wired perfectly well.
 *
 * A malformed document is treated as internal-only; the runtime validates the full
 * document at load time.
 */
export function isExposable(yaml: string): boolean {
  let decl: runDecl;
  try {
    decl = (YAML.parse(yaml) ?? {}) as runDecl;
  } catch {
    return false;
  }
  const declared = new Set((decl.env ?? []).map((e) => e?.name?.trim() ?? ""));
  return wiresInjectedPort(decl, declared);
}

/**
 * wiresInjectedPort reports whether the injected address reaches a listener that
 * serves routes. Every way for that to fail — a connector pinning its own port, one
 * reading a different variable, one whose `${HTTP_PORT}` is undeclared (a load error),
 * a pinned non-loopback host, an ambiguous binding, two connectors racing for the same
 * injected port, or simply no HTTP source at all — is a run this host would proxy into
 * a void. The rules it mirrors are the runtime's own: settings beat the environment
 * (resolvePort/resolveHost) and a source resolves its connector by explicit binding,
 * then by a lone configured instance of the type, then by starting one on demand
 * (connectorSet.resolveConnector).
 */
function wiresInjectedPort(decl: runDecl, declared: Set<string>): boolean {
  // The configured http instances, by name, each saying whether its address is this
  // host's to supply.
  const envBound = new Map<string, boolean>();
  for (const c of decl.connectors ?? []) {
    if (c?.type?.trim() !== httpConnectorType) continue;
    envBound.set(
      c.name?.trim() ?? "",
      takesEnvPort(c.settings?.port, declared) && takesEnvHost(c.settings?.host, declared),
    );
  }
  const names = [...envBound.keys()];
  // Two of them racing for the injected port is not a wiring question but a run that
  // dies on startup ("address already in use"), whichever one a source binds.
  if (names.filter((n) => envBound.get(n)).length > 1) return false;

  for (const f of decl.flows ?? []) {
    const src = f?.source;
    if (!src) continue;
    const bind = src.connector?.trim() ?? "";
    if (bind !== "") {
      // An explicit binding to a configured instance wins, whatever its type.
      const bound = envBound.get(bind);
      if (bound !== undefined) return bound;
      // Not an instance name: it only resolves if it names the type itself.
      if (bind !== httpConnectorType) continue;
    } else if (src.type?.trim() !== httpConnectorType) {
      continue;
    }
    if (names.length === 0) return true; // started on demand, straight off the env
    if (names.length === 1) return envBound.get(names[0]) ?? false;
    return false; // ambiguous: the runtime will not start it
  }
  return false;
}

/**
 * takesEnvPort reports whether a connector leaves its listen port to the environment:
 * either it sets none (the runtime reads HTTP_PORT itself) or it substitutes
 * HTTP_PORT, which resolves to the injected value because the OS environment outranks
 * a declared default. A reference only resolves if the variable is declared — an
 * undeclared one is a load error, not a listener.
 */
function takesEnvPort(raw: unknown, declared: Set<string>): boolean {
  return takesEnvVar(raw, envHTTPPort, declared);
}

/**
 * takesEnvHost reports whether a connector's bind address is one this host reaches:
 * left to the environment (unset or `${HTTP_HOST}`), or pinned to bind-all, which
 * covers the loopback this host proxies to.
 */
function takesEnvHost(raw: unknown, declared: Set<string>): boolean {
  if (typeof raw === "string" && raw.trim() === bindAllHost) return true;
  return takesEnvVar(raw, envHTTPHost, declared);
}

/**
 * takesEnvVar is the shared shape of both questions: an absent setting leaves the
 * runtime to read the variable itself, and an exact `${NAME}` reference to a declared
 * variable resolves to what was injected. Anything else — a literal, another
 * variable, an expression — pins the value beyond this host's reach.
 */
function takesEnvVar(raw: unknown, name: string, declared: Set<string>): boolean {
  if (raw === undefined || raw === null) return true;
  if (typeof raw !== "string") return false;
  const s = raw.trim();
  if (s === "") return true;
  return s === `\${${name}}` && declared.has(name);
}
