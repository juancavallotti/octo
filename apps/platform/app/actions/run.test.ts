import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The platform's RUN actions, at the boundary where the feature splits in two.
 *
 * The Testing tab's action still spawns a child of this process, and the only thing that
 * differs from standalone is the gate: a test run spawns runners, so it takes the write
 * roles rather than merely a session.
 *
 * The app-runner actions do not spawn anything at all any more — they address a dev-run
 * pod by (user, integration), which is what makes them work on a replica that did not
 * start the run. What is tested here is that boundary: which gate each action takes, that
 * the key it hands the runner carries both halves, and that a draft is refused rather
 * than started against a definition nobody saved.
 */

const USER = "user-7";

const gate = { read: vi.fn(), write: vi.fn(), user: vi.fn(), writeUser: vi.fn() };
vi.mock("./_auth", () => ({
  withRead: (fn: (s: unknown) => unknown) => {
    gate.read();
    return fn({});
  },
  withWrite: (fn: (s: unknown) => unknown) => {
    gate.write();
    return fn({});
  },
  withUser: (fn: (u: string, s: unknown) => unknown) => {
    gate.user();
    return fn(USER, {});
  },
  withWriteUser: (fn: (u: string, s: unknown) => unknown) => {
    gate.writeUser();
    return fn(USER, {});
  },
}));

// The real UNSAVED message is kept: it is what a user reads when they click Run on a
// draft, so a test asserting a stand-in would prove nothing about that.
const runner = {
  status: vi.fn(),
  start: vi.fn(),
  stop: vi.fn(),
  sync: vi.fn(),
  logs: vi.fn(),
};
vi.mock("@/app/run/remoteRunner", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/app/run/remoteRunner")>()),
  remoteRunner: runner,
}));

// What is left of the local host here: the binaries, and the one-shots they back. The
// long-running app is the remote runner's, not this module's.
const host = {
  binaries: vi.fn(),
  test: vi.fn(),
  invoke: vi.fn(),
  evalCel: vi.fn(),
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

const { UNSAVED } = await import("@/app/run/remoteRunner");
const { runStart, runStatus, runStop, runSync, runTest } = await import("./run");

/** A dev run's state, as the runner reports it. */
function runState(over: Record<string, unknown> = {}) {
  return {
    running: true,
    exposable: true,
    port: null,
    testUrl: "https://abc12def.apps.example.com",
    reloadsOnSave: true,
    ...over,
  };
}

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
  // Re-stubbed per test, not once at declaration: a test that narrows availability
  // must not narrow it for everything that follows.
  host.binaries.mockReturnValue({
    available: true,
    version: null,
    testAvailable: true,
    testVersion: null,
  });
  host.test.mockResolvedValue(hostOutcome());
  runner.status.mockResolvedValue(runState({ running: false, exposable: false, testUrl: null }));
  runner.start.mockResolvedValue(runState());
  runner.stop.mockResolvedValue(runState({ running: false, exposable: false, testUrl: null }));
  runner.sync.mockResolvedValue(runState());
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

describe("the app-runner actions", () => {
  // The gates are the same split as before the runner moved: reading state and asking for
  // a reload need a session, while starting or stopping a pod spends cluster resources.
  it("gates reads on a session and starts on the write roles", async () => {
    await runStatus(TAB, "int-1");
    await runSync(TAB, "flows: []\n", "int-1");
    expect(gate.user).toHaveBeenCalledTimes(2);
    expect(gate.writeUser).not.toHaveBeenCalled();

    await runStart(TAB, "flows: []\n", "int-1");
    await runStop(TAB, "int-1");
    expect(gate.writeUser).toHaveBeenCalledTimes(2);
  });

  // Both halves of the key, on every call: the user because a dev run is owned by one and
  // the orchestrator will not answer without it, the integration because that is what the
  // run is keyed on. The namespace rides along for the one-shots, which still need it.
  it("addresses the run by its owner and its integration", async () => {
    await runStatus(TAB, "int-1");

    expect(runner.status).toHaveBeenCalledWith({
      namespace: "ns-tab-a",
      userId: USER,
      integrationId: "int-1",
    });
  });

  // The editor asks about RUN before anything is saved, and the answer has to be "nothing
  // is running" rather than an error — otherwise the whole panel goes dark for a draft,
  // taking the one-shot debug features with it.
  it("reports on a draft without refusing", async () => {
    expect(await runStatus(TAB)).toEqual({
      ok: true,
      data: expect.objectContaining({ running: false }),
    });
    expect(runner.status).toHaveBeenCalledWith(
      expect.objectContaining({ integrationId: undefined }),
    );
  });

  // Starting is where a draft is refused: the run's sidecar pulls the SAVED definition,
  // so there would be nothing for it to fetch, and it would come up on stale YAML or none.
  it("refuses to run a draft, and says why", async () => {
    expect(await runStart(TAB, "flows: []\n")).toEqual({ ok: false, error: UNSAVED });
    expect(runner.start).not.toHaveBeenCalled();
  });

  // The editor gets one snapshot, composed from two owners: what this host can spawn, and
  // what the dev run is doing. A local binary being absent must not read as "not running".
  it("composes the host's binaries with the run's state", async () => {
    host.binaries.mockReturnValue({
      available: false,
      version: null,
      testAvailable: false,
      testVersion: null,
    });
    runner.status.mockResolvedValue(runState({ reason: "the dev run is starting" }));

    expect(await runStatus(TAB, "int-1")).toEqual({
      ok: true,
      data: {
        available: false,
        version: null,
        testAvailable: false,
        testVersion: null,
        running: true,
        exposable: true,
        port: null,
        testUrl: "https://abc12def.apps.example.com",
        reloadsOnSave: true,
        reason: "the dev run is starting",
      },
    });
  });

  // A dev run needs no local binary at all — it runs in a pod of its own — so the
  // availability gate the one-shots take must not be applied here.
  it("starts a run on a host with no runner binary", async () => {
    host.binaries.mockReturnValue({
      available: false,
      version: null,
      testAvailable: false,
      testVersion: null,
    });

    expect(await runStart(TAB, "flows: []\n", "int-1")).toEqual({
      ok: true,
      data: expect.objectContaining({ running: true }),
    });
  });

  it("turns a runner failure into a failed result", async () => {
    runner.start.mockRejectedValue(new Error("this dev run's public host is already in use"));

    expect(await runStart(TAB, "flows: []\n", "int-1")).toEqual({
      ok: false,
      error: "this dev run's public host is already in use",
    });
  });
});
