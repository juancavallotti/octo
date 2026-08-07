// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { chmod, mkdtemp, readdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { resolveParallel, resolveTimeout, test as runTests } from "./test";

const NS = "testns00";

/** Writes an executable shell script standing in for the dolphin binary. */
async function fakeBin(dir: string, name: string, body: string): Promise<string> {
  const path = join(dir, name);
  await writeFile(path, `#!/bin/sh\n${body}\n`, "utf8");
  await chmod(path, 0o755);
  return path;
}

/**
 * A fake dolphin that writes `report` to the path following --report-json and exits
 * with `code`. Written as a shell script because that is what the real one is to us: a
 * process with an argv, an exit code, and a file it leaves behind.
 *
 * `@@SUITE1@@` / `@@SUITE2@@` in the report are replaced with the actual staged suite
 * paths from argv — the real dolphin reports a suite by the path it ran, and a fixture
 * with a made-up path would let a path-matching bug pass.
 */
async function fakeDolphin(dir: string, report: unknown, code = 0): Promise<string> {
  const json = JSON.stringify(report).replace(/'/g, "'\\''");
  return fakeBin(
    dir,
    "dolphin-report",
    `
out=""
one=""
two=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--report-json" ]; then out="$a"; fi
  case "$a" in
    *_test.yaml) if [ -z "$one" ]; then one="$a"; elif [ -z "$two" ]; then two="$a"; fi ;;
  esac
  prev="$a"
done
printf '%s' '${json}' | sed -e "s#@@SUITE1@@#$one#g" -e "s#@@SUITE2@@#$two#g" > "$out"
exit ${code}
`.trim(),
  );
}

/** A report with one suite and one case of the given status. */
function report(status: string, extra: Record<string, unknown> = {}) {
  const counts: Record<string, number> = {
    passed: 0,
    failed: 0,
    errored: 0,
    skipped: 0,
    notRun: 0,
  };
  counts[status === "not-run" ? "notRun" : status] = 1;
  return {
    dolphin: "dolphin 0.0.0",
    wallMs: 5,
    totals: { cases: 1, ...counts, elapsedMs: 5 },
    suites: [
      {
        path: "@@SUITE1@@",
        flow: "orders",
        elapsedMs: 5,
        cases: [{ name: "a case", status, elapsedMs: 5, ...extra }],
      },
    ],
  };
}

const SUITE = { name: "orders", content: "flow: orders\ncases:\n  - name: a case\n" };
const YAML = "service:\n  name: t\n";

describe("test()", () => {
  let dir: string;

  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), "octo-test-"));
    process.env.OCTO_RUN_DIR = dir;
    process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-noop", "exit 0");
  });

  afterEach(() => {
    delete process.env.OCTO_BIN_PATH;
    delete process.env.DOLPHIN_BIN_PATH;
    delete process.env.OCTO_RUN_DIR;
  });

  // THE trap. dolphin exits 1 for a failed case and 2 for an errored one, and both are
  // ordinary outcomes of asking it to run tests. If `ok` tracked the exit code, the tab
  // would say "the run failed" and hide the failing case the user came to read.
  it("reports a failing case as a successful run", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeDolphin(dir, report("failed"), 1);

    const out = await runTests(NS, { yaml: YAML, suites: [SUITE] });

    expect(out.ok).toBe(true);
    expect(out.exitCode).toBe(1);
    expect(out.totals.failed).toBe(1);
    expect(out.suites[0].cases[0].status).toBe("failed");
  });

  it("reports an errored case as a successful run too", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeDolphin(dir, report("errored"), 2);

    const out = await runTests(NS, { yaml: YAML, suites: [SUITE] });

    expect(out.ok).toBe(true);
    expect(out.exitCode).toBe(2);
    expect(out.totals.errored).toBe(1);
  });

  it("fails the run when dolphin writes no report", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeBin(
      dir,
      "dolphin-refuse",
      'echo "no test file for orders.yaml" >&2; exit 2',
    );

    const out = await runTests(NS, { yaml: YAML, suites: [SUITE] });

    expect(out.ok).toBe(false);
    expect(out.error).toContain("no test file");
    expect(out.logs.join("\n")).toContain("no test file");
  });

  it("fails the run when the report is not JSON", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeBin(
      dir,
      "dolphin-garbage",
      `
out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "--report-json" ]; then shift; out="$1"; fi
  shift
done
printf 'not json' > "$out"
exit 0
`.trim(),
    );

    const out = await runTests(NS, { yaml: YAML, suites: [SUITE] });
    expect(out.ok).toBe(false);
  });

  // The report names the staged temp path. Showing that to a user would be nonsense —
  // they never made that directory and it is gone by the time they read the result.
  it("reports each suite under the name the caller gave it", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeDolphin(dir, report("passed"));

    const out = await runTests(NS, { yaml: YAML, suites: [SUITE] });

    expect(out.suites[0].name).toBe("orders");
    expect(JSON.stringify(out.suites)).not.toContain("/test-");
    expect(JSON.stringify(out.suites)).not.toContain("_test.yaml");
  });

  // dolphin SORTS its targets by suite path so a run reports its files in the same
  // order every time (discover.go's dedupe) — which is not the order they were given
  // in. Zipping the report to the input list by position mislabels every suite whose
  // name does not sort the same way, and does it silently.
  it("matches results to suites by path, not by the order they were given", async () => {
    const twoSuites = {
      dolphin: "dolphin 0.0.0",
      wallMs: 5,
      totals: { cases: 2, passed: 2, failed: 0, errored: 0, skipped: 0, notRun: 0, elapsedMs: 5 },
      suites: [
        // Reported SECOND-suite-first, as a sort would.
        { path: "@@SUITE2@@", flow: "refunds", elapsedMs: 1, cases: [] },
        { path: "@@SUITE1@@", flow: "orders", elapsedMs: 1, cases: [] },
      ],
    };
    process.env.DOLPHIN_BIN_PATH = await fakeDolphin(dir, twoSuites);

    const out = await runTests(NS, {
      yaml: YAML,
      suites: [SUITE, { name: "refunds", content: "flow: refunds\n" }],
    });

    // Each name lands on the suite that actually ran it, whatever order they came back.
    const byFlow = Object.fromEntries(out.suites.map((s) => [s.flow, s.name]));
    expect(byFlow).toEqual({ orders: "orders", refunds: "refunds" });
  });

  it("stages colliding slugs to unique paths and maps them back by path", async () => {
    const twoSuites = {
      dolphin: "dolphin 0.0.0",
      wallMs: 5,
      totals: { cases: 2, passed: 2, failed: 0, errored: 0, skipped: 0, notRun: 0, elapsedMs: 5 },
      suites: [
        // Reported second first to prove path-based mapping even with slug collisions.
        { path: "@@SUITE2@@", flow: "b", elapsedMs: 1, cases: [] },
        { path: "@@SUITE1@@", flow: "a", elapsedMs: 1, cases: [] },
      ],
    };
    process.env.DOLPHIN_BIN_PATH = await fakeDolphin(dir, twoSuites);

    const out = await runTests(NS, {
      yaml: YAML,
      suites: [
        { name: "A/B", content: "flow: a\n" },
        { name: "A B", content: "flow: b\n" },
      ],
    });

    const byFlow = Object.fromEntries(out.suites.map((s) => [s.flow, s.name]));
    expect(byFlow).toEqual({ a: "A/B", b: "A B" });
  });

  // The dedup suffix must not collide with a base that already ends in that suffix.
  // Suites "x", "x" and "x-1" would otherwise stage the second "x" and the "x-1" onto
  // one x-1_test.yaml: Promise.all's writes race, a suite's YAML is lost, and dolphin
  // (which dedupes by path) silently runs one fewer suite than asked.
  it("stages every suite to its own path even when a dedup suffix meets a real base", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeBin(
      dir,
      "dolphin-echo-paths",
      `
out=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--report-json" ]; then out="$a"; fi
  prev="$a"
done
echo "$@" >&2
printf '%s' '{"totals":{"cases":0,"passed":0,"failed":0,"errored":0,"skipped":0,"notRun":0,"elapsedMs":0},"suites":[]}' > "$out"
`.trim(),
    );

    const out = await runTests(NS, {
      yaml: YAML,
      suites: [
        { name: "x", content: "flow: x1\n" },
        { name: "x", content: "flow: x2\n" },
        { name: "x-1", content: "flow: x3\n" },
      ],
    });

    const names = out.logs
      .join(" ")
      .split(/\s+/)
      .filter((t) => t.endsWith("_test.yaml"))
      .map((t) => t.split("/").pop());
    expect(names).toHaveLength(3); // one path staged per suite
    expect(new Set(names).size).toBe(3); // and all three distinct — none overwritten
  });

  it("carries what the flow actually did back to the caller", async () => {
    const detailed = report("failed", {
      summary: "expect.body: want: {} got: {a:1}",
      failures: [{ summary: "expect.body: …", detail: "expect.body:\n  want: {}" }],
      outcome: { result: { body: { a: 1 } }, spies: { "orders.charge": [{ seq: 1 }] } },
    });
    process.env.DOLPHIN_BIN_PATH = await fakeDolphin(dir, detailed, 1);

    const c = (await runTests(NS, { yaml: YAML, suites: [SUITE] })).suites[0].cases[0];

    expect(c.failures?.[0].detail).toContain("want:");
    expect((c.outcome?.result as { body: { a: number } }).body.a).toBe(1);
    expect(c.outcome?.spies?.["orders.charge"]).toHaveLength(1);
  });

  it("passes the config, the report path, one path per suite, and --parallel 1", async () => {
    // argv goes to stderr, which comes back as `logs`: the staged dir is deleted when
    // the run ends, so anything the fake writes into it is gone before we could read it.
    process.env.DOLPHIN_BIN_PATH = await fakeBin(
      dir,
      "dolphin-echo",
      `
out=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--report-json" ]; then out="$a"; fi
  prev="$a"
done
echo "$@" >&2
printf '%s' '{"totals":{"cases":0,"passed":0,"failed":0,"errored":0,"skipped":0,"notRun":0,"elapsedMs":0},"suites":[]}' > "$out"
`.trim(),
    );

    const out = await runTests(NS, {
      yaml: YAML,
      suites: [SUITE, { name: "refunds", content: "flow: refunds\n" }],
    });
    const argv = out.logs.join(" ");

    expect(argv).toContain("--config");
    expect(argv).toContain("octo-config.yaml");
    expect(argv).toContain("--report-json");
    expect(argv).toContain("--parallel 1");
    expect(argv).toContain("orders_test.yaml");
    expect(argv).toContain("refunds_test.yaml");
  });

  // dolphin resolves octo through $OCTO_PATH as a HARD override, so pinning it makes
  // "tested against whatever octo was on the PATH" impossible rather than unlikely.
  it("pins the octo dolphin drives to the host's own", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeBin(
      dir,
      "dolphin-env",
      `
out=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--report-json" ]; then out="$a"; fi
  prev="$a"
done
echo "OCTO_PATH=$OCTO_PATH TMPDIR=$TMPDIR" >&2
printf '%s' '{"totals":{"cases":0,"passed":0,"failed":0,"errored":0,"skipped":0,"notRun":0,"elapsedMs":0},"suites":[]}' > "$out"
`.trim(),
    );

    const out = await runTests(NS, { yaml: YAML, suites: [SUITE] });
    const line = out.logs.join(" ");

    expect(line).toContain(`OCTO_PATH=${process.env.OCTO_BIN_PATH}`);
    // TMPDIR is redirected into the staged dir so dolphin's kept workdir — which it
    // leaves behind whenever a case fails — is swept up with everything else.
    expect(line).toContain("TMPDIR=");
    expect(line).toContain("/test-");
  });

  // The pinned octo and the staged TMPDIR are invariants, not preferences: a caller's
  // env must not be able to point dolphin at another octo or redirect its kept per-case
  // dirs back out of the staged tree (re-opening the /tmp leak). A plain preference like
  // LOG_LEVEL stays the caller's to set.
  it("does not let the caller's env override the pinned octo or TMPDIR", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeBin(
      dir,
      "dolphin-env-echo",
      `
out=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--report-json" ]; then out="$a"; fi
  prev="$a"
done
echo "OCTO_PATH=$OCTO_PATH TMPDIR=$TMPDIR LOG_LEVEL=$LOG_LEVEL" >&2
printf '%s' '{"totals":{"cases":0,"passed":0,"failed":0,"errored":0,"skipped":0,"notRun":0,"elapsedMs":0},"suites":[]}' > "$out"
`.trim(),
    );

    const out = await runTests(NS, {
      yaml: YAML,
      suites: [SUITE],
      env: { OCTO_PATH: "/evil/octo", TMPDIR: "/tmp/evil", LOG_LEVEL: "debug" },
    });
    const line = out.logs.join(" ");

    expect(line).toContain(`OCTO_PATH=${process.env.OCTO_BIN_PATH}`); // pinned, not /evil/octo
    expect(line).not.toContain("/evil/octo");
    expect(line).not.toContain("/tmp/evil"); // TMPDIR stayed inside the staged dir...
    expect(line).toContain("/test-"); // ...the per-run staged subdir
    expect(line).toContain("LOG_LEVEL=debug"); // a real preference the caller still sets
  });

  // dolphin's `reproduce` embeds the absolute path of the staged config and the
  // per-case debug config. Both are deleted on the way out, so it is a command that
  // cannot be run — and it would carry the server's filesystem layout to a browser.
  it("does not pass dolphin's reproduce command through to the caller", async () => {
    const withReproduce = report("failed", {
      reproduce: "/usr/local/bin/octo invoke --config /run/ns/test-abc/octo-config.yaml",
    });
    process.env.DOLPHIN_BIN_PATH = await fakeDolphin(dir, withReproduce, 1);

    const out = await runTests(NS, { yaml: YAML, suites: [SUITE] });
    const json = JSON.stringify(out.suites);

    expect(json).not.toContain("reproduce");
    expect(json).not.toContain("octo-config.yaml");
    // The things a caller CAN act on are still there.
    expect(out.suites[0].cases[0].status).toBe("failed");
  });

  it("removes the staged directory when the run ends", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeDolphin(dir, report("passed"));

    await runTests(NS, { yaml: YAML, suites: [SUITE] });

    const left = await readdir(join(dir, NS)).catch(() => []);
    expect(left.filter((f) => f.startsWith("test-"))).toEqual([]);
  });

  it("stages the resources the config declares", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeBin(
      dir,
      "dolphin-peek",
      `
out=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--report-json" ]; then out="$a"; fi
  prev="$a"
done
cat "$(dirname "$out")/.env.staging" >&2 2>/dev/null || echo "MISSING" >&2
printf '%s' '{"totals":{"cases":0,"passed":0,"failed":0,"errored":0,"skipped":0,"notRun":0,"elapsedMs":0},"suites":[]}' > "$out"
`.trim(),
    );

    const out = await runTests(NS, {
      yaml: "resources:\n  env:\n    - .env.staging\n",
      suites: [SUITE],
      resources: async (names) => names.map((name) => ({ name, content: "A=1\n" })),
    });

    expect(out.logs.join("\n")).toContain("A=1");
  });

  // The dev .env is what `invoke` injects so an ad-hoc run can reach a dev database.
  // A test must not get it: the point of writing a real _test.yaml is that `dolphin
  // test` in a terminal gives the same verdict, and a resource the config never
  // declared would make the two disagree — and could authenticate a "test" with real
  // credentials.
  it("does not inject the dev .env into a test run", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeBin(
      dir,
      "dolphin-config",
      `
out=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--report-json" ]; then out="$a"; fi
  prev="$a"
done
cat "$(dirname "$out")/octo-config.yaml" >&2
ls "$(dirname "$out")" >&2
printf '%s' '{"totals":{"cases":0,"passed":0,"failed":0,"errored":0,"skipped":0,"notRun":0,"elapsedMs":0},"suites":[]}' > "$out"
`.trim(),
    );

    const asked: string[][] = [];
    const out = await runTests(NS, {
      yaml: YAML,
      suites: [SUITE],
      resources: async (names) => {
        asked.push(names);
        return names.map((name) => ({ name, content: "SECRET=real\n" }));
      },
    });

    const seen = out.logs.join("\n");
    expect(seen).not.toContain(".env.dev");
    // The config declares nothing, so the provider is never asked for anything.
    expect(asked).toEqual([]);
  });

  it("refuses to run with no dolphin configured", async () => {
    delete process.env.DOLPHIN_BIN_PATH;
    await expect(runTests(NS, { yaml: YAML, suites: [SUITE] })).rejects.toThrow(
      /DOLPHIN_BIN_PATH/,
    );
  });

  it("does nothing, successfully, when given no suites", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeDolphin(dir, report("passed"));

    const out = await runTests(NS, { yaml: YAML, suites: [] });

    expect(out.ok).toBe(true);
    expect(out.suites).toEqual([]);
    expect(out.totals.cases).toBe(0);
  });

  it("kills a dolphin that will not finish, and says so", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeBin(dir, "dolphin-hang", "sleep 5");

    const out = await runTests(NS, { yaml: YAML, suites: [SUITE], timeoutMs: 300 });

    expect(out.ok).toBe(false);
    expect(out.timedOut).toBe(true);
    expect(out.error).toContain("did not finish");
  });

  // A caller-supplied parallel is clamped before it reaches dolphin, so it cannot open a
  // burst of connections to a real dev database.
  it("clamps a caller's parallel before handing it to dolphin", async () => {
    process.env.DOLPHIN_BIN_PATH = await fakeBin(
      dir,
      "dolphin-echo-parallel",
      `
out=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--report-json" ]; then out="$a"; fi
  prev="$a"
done
echo "$@" >&2
printf '%s' '{"totals":{"cases":0,"passed":0,"failed":0,"errored":0,"skipped":0,"notRun":0,"elapsedMs":0},"suites":[]}' > "$out"
`.trim(),
    );

    const out = await runTests(NS, { yaml: YAML, suites: [SUITE], parallel: 999 });
    expect(out.logs.join(" ")).toContain("--parallel 16"); // capped, not 999
  });
});

// The clamps are pure and worth checking at every edge, since a bad value here either
// disables the wall-clock backstop or lets a run hammer a dev database.
describe("resolveTimeout / resolveParallel", () => {
  it("honours a sensible timeout and caps an oversized one", () => {
    expect(resolveTimeout(1, 5_000)).toBe(5_000);
    expect(resolveTimeout(1, 10_000_000)).toBe(300_000); // TEST_MAX_TIMEOUT_MS
    expect(resolveTimeout(1, 300)).toBe(300); // small, but honoured
  });

  it("falls back to the suite-count default for a missing or nonsensical timeout", () => {
    expect(resolveTimeout(2)).toBe(240_000); // 2 suites * 120s, under the cap
    expect(resolveTimeout(1, 0)).toBe(120_000); // zero would disable the backstop
    expect(resolveTimeout(1, -5)).toBe(120_000);
    expect(resolveTimeout(1, Number.NaN)).toBe(120_000);
  });

  it("clamps parallel to [1, 16] and defaults a nonsensical value", () => {
    expect(resolveParallel()).toBe(1);
    expect(resolveParallel(4)).toBe(4);
    expect(resolveParallel(999)).toBe(16);
    expect(resolveParallel(0)).toBe(1);
    expect(resolveParallel(-3)).toBe(1);
    expect(resolveParallel(2.9)).toBe(2); // floored to a whole count
    expect(resolveParallel(Number.NaN)).toBe(1);
  });
});
