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
vi.mock("@/app/run/namespace", () => ({ ensureRunNamespace: async () => "ns-1" }));
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
    await runTest(req);

    expect(gate.write).toHaveBeenCalled();
    expect(gate.read).not.toHaveBeenCalled();
  });

  it("resolves the open integration's resources so a case can load them", async () => {
    await runTest(req);

    expect(host.test).toHaveBeenCalledWith("ns-1", {
      yaml: req.yaml,
      suites: req.suites,
      env: undefined,
      resources: { marker: "int-1" },
    });
  });

  // An unsaved draft has no id, so there is nothing to resolve resources against.
  it("runs a draft with no resource provider", async () => {
    await runTest({ ...req, integrationId: undefined });

    expect(host.test).toHaveBeenCalledWith(
      "ns-1",
      expect.objectContaining({ resources: undefined }),
    );
  });

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

  it("refuses when no test runner is configured", async () => {
    host.status.mockReturnValue({ available: true, testAvailable: false });

    expect(await runTest(req)).toEqual({
      ok: false,
      error: "Test runner not available (DOLPHIN_BIN_PATH unset).",
    });
    expect(host.test).not.toHaveBeenCalled();
  });

  it("refuses a request with nothing to run", async () => {
    expect(await runTest({ ...req, yaml: "" })).toEqual({ ok: false, error: "missing `yaml`" });
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
