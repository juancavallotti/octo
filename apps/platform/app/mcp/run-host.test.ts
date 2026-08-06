import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The platform's MCP run host, which is where the agent path and the editor path meet: both
 * reach the same dev run for the same (user, integration), while the one-shots stay local.
 *
 * Two things here are not simply delegation, and they are what this covers: an ad-hoc `env`
 * cannot be honoured by a runner that pulls its own config, and a log read has to be a
 * finished document rather than a follow.
 */

const runner = { start: vi.fn(), stop: vi.fn(), status: vi.fn(), sync: vi.fn(), logs: vi.fn() };
const logTail = vi.fn();
vi.mock("@/app/run/remoteRunner", () => ({ remoteRunner: runner, logTail }));

const host = {
  binaries: vi.fn(),
  invoke: vi.fn(),
  evalCel: vi.fn(),
  test: vi.fn(),
  newNamespace: vi.fn(),
};
vi.mock("@octo/run-host", () => host);

const { ENV_UNSUPPORTED, devRunMcpHost } = await import("./run-host");

const KEY = { namespace: "ns-abc123", userId: "user-7", integrationId: "int-1" };

beforeEach(() => {
  vi.clearAllMocks();
  runner.start.mockResolvedValue({ running: true });
  runner.stop.mockResolvedValue({ running: false });
  logTail.mockResolvedValue([{ seq: 0, text: "listening on 8080" }]);
});

describe("the long-running half", () => {
  // Passed through rather than dropped from the signature: the tool has the definition and
  // hands it on, and it is the backend's business that the saved copy is what runs.
  it("starts a dev run for the addressed key", async () => {
    await devRunMcpHost.start(KEY, "flows: []\n");

    expect(runner.start).toHaveBeenCalledWith(KEY, { yaml: "flows: []\n" });
  });

  // Refused, not ignored. An agent that set a value and got a run without it would go and
  // debug the flow — the one place the fault is not.
  it("refuses an ad-hoc env, and says where the values go instead", async () => {
    await expect(devRunMcpHost.start(KEY, "flows: []\n", { TOKEN: "x" })).rejects.toThrow(
      ENV_UNSUPPORTED,
    );
    expect(runner.start).not.toHaveBeenCalled();
  });

  // An agent that passed `env: {}` asked for nothing, and refusing that would be pedantry.
  it("starts normally when env is empty", async () => {
    await devRunMcpHost.start(KEY, "flows: []\n", {});

    expect(runner.start).toHaveBeenCalled();
  });

  // No resource provider: the run's sidecar fetches the integration's resources itself, so
  // staging them here would be work nothing reads.
  it("stages no resources", async () => {
    await devRunMcpHost.start(KEY, "flows: []\n", undefined, {
      resources: async () => [],
    });

    expect(runner.start).toHaveBeenCalledWith(KEY, { yaml: "flows: []\n" });
  });

  it("stops the addressed run", async () => {
    expect(await devRunMcpHost.stop(KEY)).toEqual({ running: false });
    expect(runner.stop).toHaveBeenCalledWith(KEY);
  });

  // A bounded read, not the follow the editor's log panel takes: an agent wants an answer
  // it can reason about, not a stream it has to decide when to stop draining.
  it("reads the logs as a finished document", async () => {
    expect(await devRunMcpHost.snapshot(KEY)).toEqual([{ seq: 0, text: "listening on 8080" }]);
    expect(logTail).toHaveBeenCalledWith(KEY);
    expect(runner.logs).not.toHaveBeenCalled();
  });
});

describe("the one-shots", () => {
  // Still children of this pod, and still the right place for them: they finish inside the
  // request, so no amount of fan-out ever broke them.
  it("stay local", () => {
    expect(devRunMcpHost.invoke).toBe(host.invoke);
    expect(devRunMcpHost.evalCel).toBe(host.evalCel);
    expect(devRunMcpHost.test).toBe(host.test);
    expect(devRunMcpHost.binaries).toBe(host.binaries);
  });
});
