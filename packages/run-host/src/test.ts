import { spawn } from "node:child_process";
import { mkdir, readFile, rm } from "node:fs/promises";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { namespaceDir, writeConfig } from "./session";
import {
  parseDeclaredResources,
  stageResources,
  stagedPathFor,
  type ResourceProvider,
} from "./resources";

/**
 * Running a flow's dolphin test suites, for the editor's Testing tab.
 *
 * This is the fourth consumer of the debug seam (see docs/debug-seam.md) and the only
 * one that does not spawn `octo` itself: a test case *is* a debug config plus
 * assertions, and dolphin already knows how to turn one into the other. Re-implementing
 * the assertions in TypeScript would make two sources of truth for what "the flow did
 * what it said" means, and they would drift.
 *
 * Kept out of session.ts deliberately. That module owns long-lived processes — a
 * session map, a log buffer, an allocated port, a reaper — and a test run has none of
 * them: it is one short-lived child that writes a file and exits. The only things
 * borrowed from over there are where to put the staged files and how to write a config
 * atomically.
 */

/** Default wall-clock budget for a whole run. Generous: N cases, one process each. */
const TEST_DEFAULT_TIMEOUT_MS = 120_000;

/** Grace period before a stop escalates from SIGTERM to SIGKILL (matches session.ts). */
const STOP_GRACE_MS = 3000;

/**
 * How many cases dolphin may run at once.
 *
 * One, deliberately. A connector that is not a flow *source* is started in every child
 * (see runtime/dolphin/internal/runner/run.go), so a suite over a config with a database
 * connector opens that database once per case — and an editor run often points at a
 * developer's real dev database. An interactive run is also small, and reporting its
 * cases in a stable order is worth more here than shaving a second off the clock.
 */
const DEFAULT_PARALLEL = 1;

/** What became of one case. Mirrors dolphin's report; see report/json.go. */
export type TestCaseStatus = "passed" | "failed" | "errored" | "skipped" | "not-run";

/** One expectation that did not hold. */
export interface TestFailure {
  /** One line, for a list row. */
  summary: string;
  /** The full text, which for a body diff is several lines. */
  detail: string;
}

/** What the flow actually did, as distinct from what the case wanted. */
export interface TestCaseOutcome {
  /** The flow's result message: `{event_id, variables, body}`. */
  result?: unknown;
  dropped?: boolean;
  /** The flow's own failure. */
  error?: string;
  /** What each watched block saw, keyed by address. */
  spies?: Record<string, unknown[]>;
}

/** One case, run or not. */
export interface TestCaseResult {
  name: string;
  status: TestCaseStatus;
  elapsedMs: number;
  summary?: string;
  failures?: TestFailure[];
  /** Why an ERRORED case could not be run — the suite is wrong, not the flow. */
  error?: string;
  /** octo's stderr, carried only for an errored case. */
  stderr?: string;
  /** The reason a skipped case gave. */
  skip?: string;
  /**
   * Present for every case that ran.
   *
   * Note what is deliberately NOT here: dolphin's `reproduce` command. It names the
   * staged config and the per-case debug config, both of which live in a directory this
   * function deletes on its way out — so it is a command that cannot be run, handed to
   * someone who would reasonably try. The failures and the outcome are what a caller
   * can actually act on.
   */
  outcome?: TestCaseOutcome;
}

/** One suite, run. */
export interface TestSuiteResult {
  /** The caller's name for the suite (a flow name), not the staged path. */
  name: string;
  /** The flow the suite declares. */
  flow: string;
  cases: TestCaseResult[];
}

/** What the run added up to. */
export interface TestTotals {
  cases: number;
  passed: number;
  failed: number;
  errored: number;
  skipped: number;
  notRun: number;
  elapsedMs: number;
}

/** The outcome of running one or more suites. */
export interface TestRunOutcome {
  /**
   * Whether a report came back that we could read — **not** whether the tests passed.
   *
   * dolphin exits 1 when a case failed and 2 when one errored, and both are perfectly
   * ordinary outcomes of asking it to run tests. Folding them into `ok` would tell the
   * user "the run failed" and hide the failing case they are trying to read. The
   * verdict is in {@link totals}.
   */
  ok: boolean;
  /** dolphin's exit code, or null when it was killed by a signal. */
  exitCode: number | null;
  /** True when the wall-clock backstop had to kill dolphin. */
  timedOut: boolean;
  totals: TestTotals;
  suites: TestSuiteResult[];
  /** dolphin's stderr lines. */
  logs: string[];
  /** Why the run could not be made at all, or its report not read. */
  error?: string;
}

/** One suite to run: the caller's name for it, and the YAML. */
export interface TestSuiteInput {
  /** Typically the flow name. Used to name the staged file and echoed back. */
  name: string;
  content: string;
}

/** Everything a test run needs. */
export interface TestRunArgs {
  /** The config under test, as the editor renders it. */
  yaml: string;
  /**
   * The suites to run. They come from the CALLER, not from a store, so the tab can run
   * an edit that has not been saved yet — the same reason `invoke` takes `yaml` rather
   * than reading the document back.
   */
  suites: TestSuiteInput[];
  /** Extra environment for dolphin (and so for every case). */
  env?: Record<string, string>;
  /** Wall-clock budget for the whole run. */
  timeoutMs?: number;
  /** Cases at once; see {@link DEFAULT_PARALLEL}. */
  parallel?: number;
  /** Resolves the resource files the config declares, so a case can load them. */
  resources?: ResourceProvider;
}

/** An empty tally, for a run that produced no report. */
function noTotals(): TestTotals {
  return { cases: 0, passed: 0, failed: 0, errored: 0, skipped: 0, notRun: 0, elapsedMs: 0 };
}

/** Split a captured stream into non-empty lines. */
function splitLines(text: string): string[] {
  return text.split("\n").filter((l) => l.trim() !== "");
}

/**
 * One case, rebuilt with only the fields this module publishes.
 *
 * Copied field by field rather than spread, because dolphin's report carries things a
 * caller must not receive — chiefly `reproduce`, which embeds the absolute path of the
 * staged config and the per-case debug config. Both live in a directory deleted on the
 * way out of {@link test}, so it is a command that cannot be run, and it would carry a
 * server's filesystem layout into a browser.
 *
 * The cost is that a field added to dolphin's report does not reach a caller until it is
 * added here too. That is the right default for a typed boundary: what crosses it should
 * be a decision, not whatever the subprocess happened to print.
 */
function publicCase(c: TestCaseResult): TestCaseResult {
  return {
    name: c.name,
    status: c.status,
    elapsedMs: c.elapsedMs,
    ...(c.summary !== undefined ? { summary: c.summary } : {}),
    ...(c.failures !== undefined ? { failures: c.failures } : {}),
    ...(c.error !== undefined ? { error: c.error } : {}),
    ...(c.stderr !== undefined ? { stderr: c.stderr } : {}),
    ...(c.skip !== undefined ? { skip: c.skip } : {}),
    ...(c.outcome !== undefined ? { outcome: c.outcome } : {}),
  };
}

/**
 * A filename for a suite. dolphin identifies a suite by a `*_test.yaml` name, so the
 * caller's name is reduced to something safe to put on disk and given that suffix. The
 * name at REST (a resource id, a path) is the host's business and may look nothing like
 * this; the two are deliberately not the same string.
 */
function suiteBaseName(name: string, index: number): string {
  const slug = name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  return slug || `suite-${index}`;
}

/**
 * A staged suite filename. `collision` is deterministic by input order among suites
 * sharing the same base name (0 for the first, 1 for the next, ...).
 */
function suiteFileName(base: string, collision: number): string {
  const suffix = collision === 0 ? "" : `-${collision}`;
  return `${base}${suffix}_test.yaml`;
}

/**
 * Run dolphin over `suites`, against `yaml`, and return what happened.
 *
 * The staged directory holds the config, the suites and any declared resources, and is
 * removed when the run ends.
 *
 * **The dev `.env` is deliberately NOT injected**, unlike `invoke`. The whole value of
 * the Testing tab writing a real `_test.yaml` is that `dolphin test` in a terminal gives
 * the same verdict; injecting a resource the config never declared would make the
 * editor's run and the CLI's run disagree, and would let a test quietly authenticate
 * with a developer's real credentials. A suite says what environment it needs in its own
 * `env:` block — fake by construction, reviewable, and committed with the test. A flow
 * that will not load without a variable fails here exactly as it would in CI, naming it.
 */
export async function test(ns: string, args: TestRunArgs): Promise<TestRunOutcome> {
  const bin = process.env.DOLPHIN_BIN_PATH;
  if (!bin) {
    throw new Error("DOLPHIN_BIN_PATH is not set; launch the editor with `task dev`.");
  }
  const octo = process.env.OCTO_BIN_PATH;
  if (!octo) {
    throw new Error("OCTO_BIN_PATH is not set; launch the editor with `task dev`.");
  }
  if (args.suites.length === 0) {
    return { ok: true, exitCode: 0, timedOut: false, totals: noTotals(), suites: [], logs: [] };
  }

  // Its own subdir, so a run cannot disturb the namespace's long-running runner or a
  // concurrent invoke.
  const dir = join(namespaceDir(ns), `test-${randomUUID()}`);
  await mkdir(dir, { recursive: true });

  try {
    // Only what the config DECLARES — no dev-env resource. See the note above.
    const declared = parseDeclaredResources(args.yaml);
    if (args.resources && declared.length > 0) {
      await stageResources(dir, await args.resources(declared), declared);
    }

    const configPath = join(dir, "octo-config.yaml");
    await writeConfig(configPath, args.yaml);

    const reportPath = join(dir, "report.json");
    const collisions = new Map<string, number>();
    const staged = args.suites.map((s, i) => {
      const base = suiteBaseName(s.name, i);
      const collision = collisions.get(base) ?? 0;
      collisions.set(base, collision + 1);
      return {
        suite: s,
        path: stagedPathFor(dir, suiteFileName(base, collision)),
      };
    });
    await Promise.all(staged.map(({ suite, path }) => writeConfig(path, suite.content)));

    const result = await runDolphin(bin, octo, {
      dir,
      configPath,
      reportPath,
      suitePaths: staged.map((s) => s.path),
      env: args.env,
      timeoutMs: args.timeoutMs ?? TEST_DEFAULT_TIMEOUT_MS,
      parallel: args.parallel ?? DEFAULT_PARALLEL,
    });

    // dolphin reports a suite by its staged path, and carries the staged config path
    // alongside it. The caller has never heard of that directory — it is deleted before
    // this function returns — so each suite is REBUILT with only the declared fields
    // rather than spread, which would carry those paths out untyped and into a browser.
    //
    // Matched by PATH, not by position: dolphin sorts its targets by suite path so a run
    // reports its files in the same order every time (see discover.go's dedupe), which
    // is not the order they were given in. Zipping the two lists silently mislabels
    // every suite whose name does not happen to sort the same way.
    const byPath = new Map(staged.map((s) => [s.path, s.suite.name]));
    return {
      ...result,
      suites: result.suites.map((s) => ({
        // A path we did not stage cannot happen — dolphin only ran what we gave it —
        // but falling back to the bare filename keeps a surprise legible instead of
        // producing a suite called "undefined".
        name: byPath.get(s.path) ?? s.path.split("/").pop() ?? s.path,
        flow: s.flow,
        cases: s.cases.map(publicCase),
      })),
    };
  } finally {
    await rm(dir, { recursive: true, force: true }).catch(() => {});
  }
}

/**
 * What {@link runDolphin} produces: the outcome, still carrying dolphin's own suite
 * shape. `test` translates it into the public one.
 */
type RawRunOutcome = Omit<TestRunOutcome, "suites"> & { suites: DolphinSuite[] };

/** Spawn dolphin, wait for it, and read the report it left behind. */
async function runDolphin(
  bin: string,
  octo: string,
  opts: {
    dir: string;
    configPath: string;
    reportPath: string;
    suitePaths: string[];
    env?: Record<string, string>;
    timeoutMs: number;
    parallel: number;
  },
): Promise<RawRunOutcome> {
  const argv = [
    "test",
    ...opts.suitePaths,
    "--config",
    opts.configPath,
    "--report-json",
    opts.reportPath,
    "--parallel",
    String(opts.parallel),
  ];

  const env: NodeJS.ProcessEnv = {
    ...process.env,
    // Pin the octo dolphin drives to the one this host runs. $OCTO_PATH is a hard
    // override in dolphin (runtime/dolphin/octobin.go), so this makes "tested against
    // some other octo on the PATH" structurally impossible rather than merely unlikely.
    OCTO_PATH: octo,
    // dolphin mkdtemps a per-case working directory and KEEPS it whenever anything
    // failed, so the reproduce command it prints still resolves. In a CLI that is a
    // courtesy; in a long-lived server it is an unbounded /tmp leak, one directory per
    // failing run, forever. Pointing TMPDIR at the staged dir makes it get swept with
    // everything else — at the cost of the reproduce path, which we do not offer anyway.
    TMPDIR: opts.dir,
    LOG_LEVEL: "error",
    ...(opts.env ?? {}),
  };

  const proc = spawn(bin, argv, { stdio: ["ignore", "pipe", "pipe"], env, cwd: opts.dir });

  let stdout = "";
  let stderr = "";
  proc.stdout?.setEncoding("utf8");
  proc.stdout?.on("data", (c: string) => {
    stdout += c;
  });
  proc.stderr?.setEncoding("utf8");
  proc.stderr?.on("data", (c: string) => {
    stderr += c;
  });

  let timedOut = false;
  let kill: NodeJS.Timeout | undefined;
  let force: NodeJS.Timeout | undefined;

  const exitCode = await new Promise<number | null>((resolve) => {
    const done = (code: number | null) => {
      if (kill) clearTimeout(kill);
      if (force) clearTimeout(force);
      resolve(code);
    };
    kill = setTimeout(() => {
      timedOut = true;
      proc.kill("SIGTERM");
      force = setTimeout(() => {
        try {
          proc.kill("SIGKILL");
        } catch {
          // already gone
        }
      }, STOP_GRACE_MS);
    }, opts.timeoutMs);
    proc.on("error", (err) => {
      stderr += `✖ failed to start dolphin: ${err.message}\n`;
      done(null);
    });
    proc.on("exit", (code) => done(code));
  });

  const logs = splitLines(stderr);
  if (timedOut) {
    return {
      ok: false,
      exitCode,
      timedOut,
      totals: noTotals(),
      suites: [],
      logs,
      error: `the test run did not finish within ${Math.round(opts.timeoutMs / 1000)}s`,
    };
  }

  const report = await readReport(opts.reportPath);
  if (!report) {
    // No report and no timeout means dolphin refused the run before it started —
    // unreadable suite, unusable octo, bad flag. Its stderr is the whole story, and
    // stdout may carry the console report's first lines.
    return {
      ok: false,
      exitCode,
      timedOut,
      totals: noTotals(),
      suites: [],
      logs,
      error: logs.at(-1) ?? splitLines(stdout).at(-1) ?? "dolphin wrote no report",
    };
  }

  return { ok: true, exitCode, timedOut, totals: report.totals, suites: report.suites, logs };
}

/**
 * One suite as DOLPHIN reports it — by the staged path it ran, which is how a result is
 * matched back to the suite the caller asked for. Kept separate from the public
 * {@link TestSuiteResult}, which carries no paths at all.
 */
interface DolphinSuite {
  path: string;
  flow: string;
  cases: TestCaseResult[];
}

/** The parts of dolphin's report this module reads. */
interface DolphinReport {
  totals: TestTotals;
  suites: DolphinSuite[];
}

/** Read and parse the report, or null when there is none to read. */
async function readReport(path: string): Promise<DolphinReport | null> {
  let raw: string;
  try {
    raw = await readFile(path, "utf8");
  } catch {
    return null;
  }
  try {
    const doc = JSON.parse(raw) as Partial<DolphinReport>;
    if (!doc || typeof doc !== "object" || !doc.totals || !Array.isArray(doc.suites)) {
      return null;
    }
    return { totals: doc.totals, suites: doc.suites };
  } catch {
    return null;
  }
}
