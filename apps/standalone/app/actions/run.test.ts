import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The Testing tab's server action. What is worth pinning here is the boundary: which
 * requests are refused before a process is spawned, and exactly which fields of
 * run-host's result are allowed to reach the browser.
 */

const host = {
  status: vi.fn(),
  test: vi.fn(),
  invoke: vi.fn(),
  evalCel: vi.fn(),
  start: vi.fn(),
  stop: vi.fn(),
  sync: vi.fn(),
  probeVersion: vi.fn(),
  probeTestVersion: vi.fn(),
};
vi.mock("@octo/run-host", () => host);
vi.mock("../run/namespace", () => ({ ensureRunNamespace: async () => "ns-1" }));
vi.mock("../run/resources", () => ({ fsResourceProvider: { marker: "fs" } }));

const { runTest } = await import("./run");

/** A report as run-host hands it over — including the field that must not cross. */
function hostOutcome(over: Record<string, unknown> = {}) {
  return {
    ok: true,
    exitCode: 1,
    timedOut: false,
    totals: {
      cases: 2,
      passed: 1,
      failed: 1,
      errored: 0,
      skipped: 0,
      notRun: 0,
      elapsedMs: 20,
    },
    suites: [{ name: "orders", flow: "orders", cases: [] }],
    logs: ["a log line"],
    ...over,
  };
}

const req = {
  yaml: "flows: []\n",
  suites: [{ name: "orders", content: "flow: orders\ncases: []\n" }],
};

beforeEach(() => {
  vi.clearAllMocks();
  host.status.mockReturnValue({ available: true, testAvailable: true });
  host.test.mockResolvedValue(hostOutcome());
});

describe("runTest", () => {
  it("runs the suites and passes the resource provider through", async () => {
    const result = await runTest({ ...req, env: { A: "1" } });

    expect(result.ok).toBe(true);
    expect(host.test).toHaveBeenCalledWith("ns-1", {
      yaml: req.yaml,
      suites: req.suites,
      env: { A: "1" },
      resources: { marker: "fs" },
    });
  });

  // dolphin's exit code is a detail of how the run was made. The UI reasons about the
  // tally, and a field that crosses a typed boundary should be a decision.
  it("does not leak the exit code to the browser", async () => {
    const result = await runTest(req);

    expect(result.ok && Object.keys(result.data).sort()).toEqual([
      "logs",
      "ok",
      "suites",
      "timedOut",
      "totals",
    ]);
  });

  // dolphin exits non-zero when a case fails, and a failing case is the whole point of
  // asking. Reporting that as a failed action would hide the report the user came for.
  it("reports a run whose tests failed as a successful run", async () => {
    const result = await runTest(req);

    expect(result).toEqual({
      ok: true,
      data: {
        ok: true,
        timedOut: false,
        totals: hostOutcome().totals,
        suites: [{ name: "orders", flow: "orders", cases: [] }],
        logs: ["a log line"],
      },
    });
  });

  it("carries an unreadable-report error through", async () => {
    host.test.mockResolvedValue(hostOutcome({ ok: false, error: "no report was written" }));

    const result = await runTest(req);
    expect(result).toMatchObject({ ok: true, data: { ok: false, error: "no report was written" } });
  });

  // dolphin can be absent while octo is present, so this is its own gate.
  it("refuses when no test runner is configured", async () => {
    host.status.mockReturnValue({ available: true, testAvailable: false });

    expect(await runTest(req)).toEqual({
      ok: false,
      error: "Test runner not available (DOLPHIN_BIN_PATH unset).",
    });
    expect(host.test).not.toHaveBeenCalled();
  });

  it("refuses a request with nothing to run", async () => {
    expect(await runTest({ ...req, yaml: "  " })).toEqual({
      ok: false,
      error: "missing `yaml`",
    });
    expect(await runTest({ ...req, suites: [] })).toEqual({
      ok: false,
      error: "no test suites to run",
    });
    expect(host.test).not.toHaveBeenCalled();
  });

  it("turns a thrown runner error into a failed result", async () => {
    host.test.mockRejectedValue(new Error("spawn ENOENT"));

    expect(await runTest(req)).toEqual({ ok: false, error: "spawn ENOENT" });
  });
});
