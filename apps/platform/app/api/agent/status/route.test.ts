// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * Whether the chat launcher renders, which is a question about liveness and not
 * about freshness.
 *
 * The two were one field once: the launcher gated on `state === "deployed"`, and
 * the orchestrator reports `update_available` for a running agent as soon as the
 * binary ships a newer bundle than the one rolled out. So upgrading the platform
 * hid the panel everywhere until somebody visited Admin and pressed Roll out —
 * while the chat route behind it, which only ever needed the address, would have
 * worked fine.
 */

const auth = { currentWriteUserId: vi.fn() };
vi.mock("@/app/actions/_auth", () => auth);

vi.mock("@/app/auth/guard", () => ({
  AuthError: class AuthError extends Error {},
  ForbiddenError: class ForbiddenError extends Error {},
}));

const status = { fetchAgentStatus: vi.fn() };
vi.mock("@/app/actions/client/agentUrl", () => status);

const { GET } = await import("./route");

async function available(): Promise<boolean> {
  const res = await GET();
  return ((await res.json()) as { available: boolean }).available;
}

describe("GET /api/agent/status", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    auth.currentWriteUserId.mockResolvedValue("u-1");
  });

  it("is available when a deployment is running the current bundle", async () => {
    status.fetchAgentStatus.mockResolvedValue({
      state: "deployed",
      internalUrl: "http://octo-int-dr-octo:8080",
    });
    expect(await available()).toBe(true);
  });

  // The regression: a platform upgrade changes the shipped bundle's digest, so a
  // healthy agent reports update_available. The panel must not disappear for it.
  it("stays available when the platform ships a newer bundle than the one running", async () => {
    status.fetchAgentStatus.mockResolvedValue({
      state: "update_available",
      internalUrl: "http://octo-int-dr-octo:8080",
    });
    expect(await available()).toBe(true);
  });

  it("is unavailable when the workload is failing", async () => {
    status.fetchAgentStatus.mockResolvedValue({
      state: "failed",
      internalUrl: "http://octo-int-dr-octo:8080",
    });
    expect(await available()).toBe(false);
  });

  // An address is what says a deployment was actually found: Status clears it when
  // the deployment is gone underneath the row that named it.
  it("is unavailable when nothing is deployed", async () => {
    status.fetchAgentStatus.mockResolvedValue({ state: "installed" });
    expect(await available()).toBe(false);
  });

  it("is unavailable when the orchestrator cannot be read", async () => {
    status.fetchAgentStatus.mockResolvedValue(null);
    expect(await available()).toBe(false);
  });

  // A reader who cannot use the panel is not shown a button that would answer 403,
  // and is not told whether the agent exists either.
  it("is unavailable to a caller without the write role, without asking the orchestrator", async () => {
    auth.currentWriteUserId.mockRejectedValue(new Error("forbidden"));
    expect(await available()).toBe(false);
    expect(status.fetchAgentStatus).not.toHaveBeenCalled();
  });
});
