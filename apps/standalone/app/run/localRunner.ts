import { spawn } from "node:child_process";
import { mkdir, rm } from "node:fs/promises";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import {
  allocateAdminPort,
  allocatePort,
  isExposable,
  releaseAdminPort,
  releasePort,
} from "./ports";
import { followLogs } from "./logStream";
import {
  ensureKillOnExit,
  session,
  status,
  statusOf,
  withNamespaceLock,
} from "./session";
import {
  effectiveResourceNames,
  injectDevEnvResource,
  namespaceDir,
  octoBin,
  resolveAndStage,
  sameNameSet,
  stageResources,
  terminate,
  writeConfig,
  type AppRunner,
  type RunKey,
  type RunResourceOptions,
  type RunState,
} from "@octo/run-host";

/**
 * The local {@link AppRunner}: it owns the `octo` processes running as children of this
 * one. The editor pushes YAML, this spawns `octo run -config <file> -watch`, captures
 * stdout/stderr as log lines, and lets clients replay the buffer and subscribe to new
 * lines. Editing the document re-writes the same config file so the runner hot-reloads.
 *
 * Runs are keyed by a per-user namespace slug (see namespace.ts) so concurrent editor
 * users don't disturb one another: each namespace owns an independent process, config
 * file, and log buffer. The session records themselves — and the lock that keeps the
 * operations below from interleaving — live in `session.ts`; this module is the half
 * that spawns and kills.
 *
 * **Why this lives in the app and not in @octo/run-host.** Everything here is inherently
 * process-local — a child handle, a port from a pool this process owns, a log buffer in
 * this heap — and only one host can use it: an app served by several replicas with no
 * session affinity would start a run on one and find nothing on the next. So it is not
 * shared code that happens to sit in a package; it is *this app's* answer to a question
 * the platform answers differently (`apps/platform/app/run/remoteRunner.ts`, a pod the
 * orchestrator owns). What the two share is the interface and the staging, binary and
 * one-shot helpers they both genuinely use.
 */

/**
 * Start (or restart) the namespace's runner with the given rendered config YAML.
 * `devEnv` (the editor's "Dev .env" values) is injected into the spawned process's
 * environment below and never written to disk — the `env` object is local to this
 * call. The Go runtime resolves OS env before declared defaults, so this suffices.
 */
export function start(
  ns: string,
  yaml: string,
  devEnv?: Record<string, string>,
  opts?: RunResourceOptions,
): Promise<RunState> {
  return withNamespaceLock(ns, () => startImpl(ns, yaml, devEnv, opts));
}

async function startImpl(
  ns: string,
  yaml: string,
  devEnv?: Record<string, string>,
  opts?: RunResourceOptions,
): Promise<RunState> {
  const bin = octoBin();

  await stopImpl(ns); // tear down any previous generation first

  const s = session(ns);
  s.logs.reset(); // fresh buffer per run; seq stays monotonic so clients still dedupe

  const dir = namespaceDir(ns);
  await mkdir(dir, { recursive: true });

  // Stage the config's declared resources into the run dir first: the runtime's
  // fs loader roots at the config directory, and the config write below is what
  // triggers its initial load, so the files must already be present.
  const { declared, staged } = await resolveAndStage(dir, yaml, opts?.resources);
  s.stagedResources = staged;
  s.declaredResources = declared;

  const configPath = join(dir, `octo-editor-${randomUUID()}.yaml`);
  await writeConfig(configPath, injectDevEnvResource(yaml));
  s.configPath = configPath;

  // The run's ports, taken together so a failure to get one does not strand the
  // other. Each is recorded on the session as it is taken, which is what lets the
  // rollback below hand it back.
  const exposable = isExposable(yaml);
  let adminPort: number;
  try {
    // A networked integration (one that declares HTTP_PORT) gets a real port from
    // the pool, injected as HTTP_PORT so the BFF can proxy to it. HTTP_HOST is the
    // loopback because only the same-pod proxy needs to reach it. Internal-only runs
    // (no HTTP_PORT) get no port and stay unexposed.
    s.exposable = exposable;
    s.port = exposable ? allocatePort() : null;

    // Every run — networked or not — also gets an admin port for the runtime's
    // observability service, which is on by default and otherwise binds the same
    // fixed :39999 in every run on this host: the first run would take it and every
    // later one would come up without probes or metrics. Loopback, because only this
    // host reaches it.
    adminPort = allocateAdminPort();
    s.adminPort = adminPort;
  } catch (err) {
    // An exhausted pool ends this start, so roll the whole thing back: stop()
    // returns whichever port was taken and removes the config and staged resources
    // (env files hold secrets) instead of leaving them for whoever calls stop next.
    // Repeated failed starts would otherwise eat the pool a port at a time.
    await stopImpl(ns);
    throw err;
  }
  const port = s.port;

  // Base process env, then the dev env values; the port wiring is applied last so
  // a stray dev value can't clobber it.
  const env: NodeJS.ProcessEnv = { ...process.env, ...(devEnv ?? {}) };
  if (port !== null) {
    env.HTTP_PORT = String(port);
    env.HTTP_HOST = "127.0.0.1";
  }
  env.OCTO_OBSERVABILITY_ADDR = `127.0.0.1:${adminPort}`;

  s.logs.push(`▶ starting octo — ${configPath}`);
  if (port !== null) {
    s.logs.push(`🔗 test your integration at /editor/runs/${ns}/`);
  }
  const proc = spawn(bin, ["run", "-config", configPath, "-watch"], {
    stdio: ["ignore", "pipe", "pipe"],
    env,
  });
  s.proc = proc;
  s.logs.pipe(proc.stdout);
  s.logs.pipe(proc.stderr);

  s.exit = new Promise<void>((resolve) => {
    const finish = () => {
      if (s.proc === proc) {
        s.proc = null;
        // Free this generation's ports when it exits on its own (crash/exit).
        if (port !== null && s.port === port) {
          releasePort(port);
          s.port = null;
        }
        if (s.adminPort === adminPort) {
          releaseAdminPort(adminPort);
          s.adminPort = null;
        }
      }
      resolve();
    };
    proc.on("error", (err) => {
      s.logs.push(`✖ failed to start runner: ${err.message}`);
      finish();
    });
    // Resolve on "exit" (process gone) rather than "close" (stdio EOF) so stop()
    // stays responsive even if a child inherits and holds the output pipes.
    proc.on("exit", (code, signal) => {
      s.logs.push(
        `■ runner exited (${signal ? `signal ${signal}` : `code ${code ?? 0}`})`,
      );
      finish();
    });
  });

  ensureKillOnExit();
  return statusOf(s);
}

/**
 * Re-render the config the namespace's runner is watching, triggering a hot reload.
 * No-op if stopped. When the config's declared resource-name set has changed since
 * the last staging and a provider is given, the resources are re-resolved and
 * re-staged (and files no longer declared are removed) before the config is
 * rewritten — an ordinary content-only edit skips the provider entirely.
 */
export function sync(
  ns: string,
  yaml: string,
  opts?: RunResourceOptions,
): Promise<RunState> {
  return withNamespaceLock(ns, () => syncImpl(ns, yaml, opts));
}

async function syncImpl(
  ns: string,
  yaml: string,
  opts?: RunResourceOptions,
): Promise<RunState> {
  const s = session(ns);
  if (!s.proc || !s.configPath) return statusOf(s);

  const declared = effectiveResourceNames(yaml);
  if (opts?.resources && !sameNameSet(declared, s.declaredResources)) {
    const dir = namespaceDir(ns);
    const files = await opts.resources(declared);
    const nextStaged = await stageResources(dir, files, declared);
    // Remove files that were staged before but are no longer declared/supplied.
    const keep = new Set(nextStaged);
    for (const path of s.stagedResources) {
      if (!keep.has(path)) await rm(path, { force: true }).catch(() => {});
    }
    s.stagedResources = nextStaged;
    s.declaredResources = declared;
  }

  await writeConfig(s.configPath, injectDevEnvResource(yaml));
  return statusOf(s);
}

/** Stop the namespace's runner (SIGTERM, then SIGKILL after a grace period) and remove its config. */
export function stop(ns: string): Promise<RunState> {
  return withNamespaceLock(ns, () => stopImpl(ns));
}

async function stopImpl(ns: string): Promise<RunState> {
  const s = session(ns);
  const proc = s.proc;
  if (proc) {
    const cancelKill = terminate(proc);
    try {
      await s.exit;
    } finally {
      cancelKill();
    }
  }
  s.proc = null;
  s.exit = null;
  if (s.port !== null) {
    releasePort(s.port);
    s.port = null;
  }
  if (s.adminPort !== null) {
    releaseAdminPort(s.adminPort);
    s.adminPort = null;
  }
  s.exposable = false;
  if (s.configPath) {
    await rm(s.configPath, { force: true }).catch(() => {});
    s.configPath = null;
  }
  // Remove staged resource files (env resources may hold secrets), like the config.
  for (const path of s.stagedResources) {
    await rm(path, { force: true }).catch(() => {});
  }
  s.stagedResources = [];
  s.declaredResources = [];
  return statusOf(s);
}

/**
 * The local backend as an {@link AppRunner}. A thin adapter over the module functions
 * above rather than a reimplementation: those functions are the API every existing
 * caller uses, and having two code paths to the same child process is how the two start
 * to disagree.
 */
export const localRunner: AppRunner = {
  status: async (key: RunKey) => status(key.namespace),
  start: (key, args) =>
    start(key.namespace, args.yaml, args.devEnv, { resources: args.resources }),
  stop: (key) => stop(key.namespace),
  sync: (key, args) => sync(key.namespace, args.yaml, { resources: args.resources }),
  logs: (key, opts) => followLogs(key.namespace, opts),
};
