import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The Testing tab's server action on the platform. Same boundary as standalone's, plus
 * the thing that is different here: a test run spawns runners, so it takes the write
 * roles rather than merely a session.
 */

const gate = { read: vi.fn(), write: vi.fn() };
vi.mock("./_auth", () => ({
  withRead: (fn: (s: unknown) => unknown) => {
    gate.read();
    return fn({});
  },
  withWrite: (fn: (s: unknown) => unknown) => {
    gate.write();
    return fn({});
  },
}));

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

// Stands in for the real cookie+tab derivation: what matters at this boundary is
// that the action hands the caller's tab id over and runs whatever namespace comes
// back, so two tabs never land on one runner.
const namespace = {
  ensureRunNamespace: vi.fn(async (tabId?: string) => (tabId ? `ns-${tabId}` : "ns-cookie")),
};
vi.mock("@/app/run/namespace", () => namespace);
vi.mock("@/app/lib/runResources", () => ({
  orchestratorResourceProvider: (id: string) => ({ marker: id }),
}));

const { runTest } = await import("./run");

function hostOutcome(over: Record<string, unknown> = {}) {
  return {
    ok: true,
    exitCode: 1,
    timedOut: false,
    totals: {
      cases: 1,
      passed: 0,
      failed: 1,
      errored: 0,
      skipped: 0,
      notRun: 0,
      elapsedMs: 10,
    },
    suites: [{ name: "orders", flow: "orders", cases: [] }],
    logs: [],
    ...over,
  };
}

const TAB = "tab-a";

const req = {
  yaml: "flows: []\n",
  integrationId: "int-1",
  suites: [{ name: "orders", content: "flow: orders\ncases: []\n" }],
};

beforeEach(() => {
  vi.clearAllMocks();
  host.status.mockReturnValue({ available: true, testAvailable: true });
  host.test.mockResolvedValue(hostOutcome());
});

describe("runTest", () => {
  // Spawning a runner is a write, whatever it is called.
  it("takes the write roles, not just a session", async () => {
    await runTest(TAB, req);

    expect(gate.write).toHaveBeenCalled();
    expect(gate.read).not.toHaveBeenCalled();
  });

  // Two tabs of one browser must reach two runners — otherwise starting a suite in
  // one tab tears down whatever the other had running.
  it("runs each tab against its own namespace", async () => {
    await runTest(TAB, req);
    await runTest("tab-b", req);

    expect(namespace.ensureRunNamespace).toHaveBeenNthCalledWith(1, TAB);
    expect(host.test.mock.calls.map((c) => c[0])).toEqual(["ns-tab-a", "ns-tab-b"]);
  });

  it("resolves the open integration's resources so a case can load them", async () => {
    await runTest(TAB, req);

    expect(host.test).toHaveBeenCalledWith("ns-tab-a", {
      yaml: req.yaml,
      suites: req.suites,
      env: undefined,
      resources: { marker: "int-1" },
    });
  });

  // An unsaved draft has no id, so there is nothing to resolve resources against.
  it("runs a draft with no resource provider", async () => {
    await runTest(TAB, { ...req, integrationId: undefined });

    expect(host.test).toHaveBeenCalledWith(
      "ns-tab-a",
      expect.objectContaining({ resources: undefined }),
    );
  });

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

  it("refuses when no test runner is configured", async () => {
    host.status.mockReturnValue({ available: true, testAvailable: false });

    expect(await runTest(TAB, req)).toEqual({
      ok: false,
      error: "Test runner not available (DOLPHIN_BIN_PATH unset).",
    });
    expect(host.test).not.toHaveBeenCalled();
  });

  it("refuses a request with nothing to run", async () => {
    expect(await runTest(TAB, { ...req, yaml: "" })).toEqual({ ok: false, error: "missing `yaml`" });
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
