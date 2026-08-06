import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The Testing tab's server action. What is worth pinning here is the boundary: which
 * requests are refused before a process is spawned, and exactly which fields of
 * run-host's result are allowed to reach the browser.
 */

const host = {
  // The host binaries are reported apart from the run itself, so the stub answers
  // both: a test that only mocked status() would fail the availability gate.
  binaries: vi.fn(),
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

// Stands in for the real cookie+tab derivation: what matters at this boundary is
// that the action hands the caller's tab id over and runs whatever namespace comes
// back, so two tabs never land on one runner.
const namespace = {
  ensureRunNamespace: vi.fn(async (tabId?: string) => (tabId ? `ns-${tabId}` : "ns-cookie")),
};
vi.mock("../run/namespace", () => namespace);
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

const TAB = "tab-a";

const req = {
  yaml: "flows: []\n",
  suites: [{ name: "orders", content: "flow: orders\ncases: []\n" }],
};

beforeEach(() => {
  vi.clearAllMocks();
  // Re-stubbed per test, not once at declaration: a test that narrows availability
  // must not narrow it for everything that follows.
  host.binaries.mockReturnValue({
    available: true,
    version: null,
    testAvailable: true,
    testVersion: null,
  });
  host.status.mockReturnValue({ running: false, exposable: false, port: null, testUrl: null });
  host.test.mockResolvedValue(hostOutcome());
});

describe("runTest", () => {
  // Two tabs of one browser must reach two runners — otherwise starting a suite in
  // one tab tears down whatever the other had running.
  it("runs each tab against its own namespace", async () => {
    await runTest(TAB, req);
    await runTest("tab-b", req);

    expect(namespace.ensureRunNamespace).toHaveBeenNthCalledWith(1, TAB);
    expect(host.test.mock.calls.map((c) => c[0])).toEqual(["ns-tab-a", "ns-tab-b"]);
  });

  it("runs the suites and passes the resource provider through", async () => {
    const result = await runTest(TAB, { ...req, env: { A: "1" } });

    expect(result.ok).toBe(true);
    expect(host.test).toHaveBeenCalledWith("ns-tab-a", {
      yaml: req.yaml,
      suites: req.suites,
      env: { A: "1" },
      resources: { marker: "fs" },
    });
  });

  // dolphin's exit code is a detail of how the run was made. The UI reasons about the
  // tally, and a field that crosses a typed boundary should be a decision.
  it("does not leak the exit code to the browser", async () => {
    const result = await runTest(TAB, req);

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
    const result = await runTest(TAB, req);

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

    const result = await runTest(TAB, req);
    expect(result).toMatchObject({ ok: true, data: { ok: false, error: "no report was written" } });
  });

  // dolphin can be absent while octo is present, so this is its own gate.
  it("refuses when no test runner is configured", async () => {
    host.binaries.mockReturnValue({
      available: true,
      version: null,
      testAvailable: false,
      testVersion: null,
    });

    expect(await runTest(TAB, req)).toEqual({
      ok: false,
      error: "Test runner not available (DOLPHIN_BIN_PATH unset).",
    });
    expect(host.test).not.toHaveBeenCalled();
  });

  it("refuses a request with nothing to run", async () => {
    expect(await runTest(TAB, { ...req, yaml: "  " })).toEqual({
      ok: false,
      error: "missing `yaml`",
    });
    expect(await runTest(TAB, { ...req, suites: [] })).toEqual({
      ok: false,
      error: "no test suites to run",
    });
    expect(host.test).not.toHaveBeenCalled();
  });

  it("turns a thrown runner error into a failed result", async () => {
    host.test.mockRejectedValue(new Error("spawn ENOENT"));

    expect(await runTest(TAB, req)).toEqual({ ok: false, error: "spawn ENOENT" });
  });
});
