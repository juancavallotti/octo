// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const exchangeIdToken = vi.fn();
const refreshOctoToken = vi.fn();
vi.mock("@/app/actions/client/iam", () => ({ exchangeIdToken, refreshOctoToken }));

const { keepFresh, signInExchange } = await import("./octoToken");

/** What iam answers with on a successful mint. */
function minted(over: { expiresAt?: string; roles?: string[]; token?: string } = {}) {
  return {
    ok: true as const,
    data: {
      token: over.token ?? "octo-token-2",
      expiresAt: over.expiresAt ?? new Date(Date.now() + 3600_000).toISOString(),
      user: {
        id: "user-1",
        email: "a@example.com",
        name: "Ada",
        roles: over.roles ?? ["platform:admin"],
        createdAt: "",
        lastLoginAt: "",
      },
    },
  };
}

/** A session holding a token that expires in `ms`. */
function session(ms: number) {
  return { octoToken: "octo-token-1", octoExpiresAt: Date.now() + ms, roles: ["platform:monitor"] };
}

beforeEach(() => {
  vi.spyOn(console, "warn").mockImplementation(() => {});
  vi.spyOn(console, "error").mockImplementation(() => {});
});
afterEach(() => {
  vi.restoreAllMocks();
  exchangeIdToken.mockReset();
  refreshOctoToken.mockReset();
});

describe("signInExchange", () => {
  it("puts the minted token, its expiry, the roles and the user id on the session", async () => {
    exchangeIdToken.mockResolvedValue(minted());
    const fields = await signInExchange("idp-token");
    expect(exchangeIdToken).toHaveBeenCalledWith("idp-token");
    expect(fields).toMatchObject({
      octoToken: "octo-token-2",
      roles: ["platform:admin"],
      userId: "user-1",
    });
    expect(fields!.octoExpiresAt).toBeGreaterThan(Date.now());
  });

  // A session that looks signed in and can call nothing is worse than an error at
  // the door, so the sign-in is refused rather than half-completed.
  it("refuses the sign-in when iam cannot mint", async () => {
    exchangeIdToken.mockResolvedValue({ ok: false, error: "IAM_URL unset" });
    await expect(signInExchange("idp-token")).resolves.toBeNull();
  });
});

describe("keepFresh", () => {
  it("leaves a token that is nowhere near expiry alone", async () => {
    const fields = session(30 * 60_000);
    await expect(keepFresh(fields)).resolves.toBe(fields);
    expect(refreshOctoToken).not.toHaveBeenCalled();
  });

  it("renews a token inside the lead window and takes the new roles", async () => {
    refreshOctoToken.mockResolvedValue(minted({ roles: ["platform:developer"] }));
    const fresh = await keepFresh(session(60_000));
    expect(refreshOctoToken).toHaveBeenCalledWith("octo-token-1");
    expect(fresh).toMatchObject({ octoToken: "octo-token-2", roles: ["platform:developer"] });
  });

  it("renews a token that has already expired, and lets iam judge the window", async () => {
    refreshOctoToken.mockResolvedValue(minted());
    await expect(keepFresh(session(-60_000))).resolves.toMatchObject({
      octoToken: "octo-token-2",
    });
  });

  it("ends the session when iam refuses the token", async () => {
    refreshOctoToken.mockResolvedValue({ ok: false, error: "expired", status: 401 });
    await expect(keepFresh(session(60_000))).resolves.toBeNull();
  });

  // The row of the taxonomy that matters most: getting this wrong turns an iam
  // rolling restart into a platform-wide forced sign-out.
  it("keeps the existing token when iam cannot be reached", async () => {
    const fields = session(60_000);
    for (const failure of [
      { ok: false, error: "service unavailable", status: 503 },
      { ok: false, error: "request failed: ECONNREFUSED" },
      { ok: false, error: "sign-in not configured (IAM_URL unset)" },
    ]) {
      refreshOctoToken.mockResolvedValue(failure);
      await expect(keepFresh(fields)).resolves.toBe(fields);
    }
  });

  it("ends a session that carries no token at all", async () => {
    await expect(keepFresh({})).resolves.toBeNull();
    await expect(keepFresh({ roles: ["platform:admin"] })).resolves.toBeNull();
    expect(refreshOctoToken).not.toHaveBeenCalled();
  });

  // A render calling auth() several times must not ask iam several times for a
  // token it is about to discard — see the note in the module about the proxy.
  it("coalesces concurrent renewals of the same token into one call", async () => {
    refreshOctoToken.mockResolvedValue(minted());
    const fields = session(60_000);
    await Promise.all([keepFresh(fields), keepFresh(fields), keepFresh(fields)]);
    expect(refreshOctoToken).toHaveBeenCalledTimes(1);
  });

  it("does not hold the coalesced attempt past its completion", async () => {
    refreshOctoToken.mockResolvedValue(minted());
    const fields = session(60_000);
    await keepFresh(fields);
    await keepFresh(fields);
    expect(refreshOctoToken).toHaveBeenCalledTimes(2);
  });
});
