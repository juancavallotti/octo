import { beforeEach, describe, expect, it, vi } from "vitest";

// The callbacks are three lines of plumbing over app/auth/octoToken.ts, which owns
// the policy and is tested on its own. What is asserted here is the plumbing: that
// a sign-in is told apart from a later session read, that a refusal ends the
// session, and that the credential does not reach the browser.
const { keepFresh, signInExchange } = vi.hoisted(() => ({
  signInExchange: vi.fn(),
  keepFresh: vi.fn(),
}));
vi.mock("@/app/auth/octoToken", () => ({ signInExchange, keepFresh }));

import { authConfig } from "./auth.config";

/** Invoke the jwt callback with only the fields it actually reads. */
function runJwt(args: {
  token: Record<string, unknown>;
  account?: Record<string, unknown> | null;
}) {
  return (authConfig.callbacks!.jwt as (a: unknown) => Promise<unknown>)(args);
}

/** Invoke the session callback the same way. */
function runSession(args: {
  session: Record<string, unknown>;
  token: Record<string, unknown>;
}) {
  return (authConfig.callbacks!.session as (a: unknown) => unknown)(args);
}

const fields = {
  octoToken: "octo-token",
  octoExpiresAt: Date.now() + 3600_000,
  roles: ["platform:admin"],
  userId: "user-1",
};

beforeEach(() => {
  signInExchange.mockReset();
  keepFresh.mockReset();
});

describe("auth.config jwt callback", () => {
  // The raw provider token is on `account`, not `profile`, and `account` is
  // present only on sign-in — which is what makes the exchange happen once per
  // session rather than once per request.
  it("exchanges the provider's id token on sign-in and carries the result", async () => {
    signInExchange.mockResolvedValue(fields);
    const token = await runJwt({
      token: { sub: "authjs-sub" },
      account: { id_token: "idp-token" },
    });

    expect(signInExchange).toHaveBeenCalledWith("idp-token");
    expect(keepFresh).not.toHaveBeenCalled();
    expect(token).toMatchObject(fields);
  });

  it("refuses the sign-in when no platform token can be obtained", async () => {
    signInExchange.mockResolvedValue(null);
    await expect(
      runJwt({ token: { sub: "authjs-sub" }, account: { id_token: "idp-token" } }),
    ).resolves.toBeNull();
  });

  it("considers a renewal on every later read", async () => {
    keepFresh.mockResolvedValue(fields);
    const token = await runJwt({ token: { sub: "authjs-sub", ...fields }, account: null });

    expect(keepFresh).toHaveBeenCalledWith(expect.objectContaining({ octoToken: "octo-token" }));
    expect(signInExchange).not.toHaveBeenCalled();
    expect(token).toMatchObject(fields);
  });

  // Returning null is how Auth.js is told to clear the cookies.
  it("ends the session when the token can no longer be renewed", async () => {
    keepFresh.mockResolvedValue(null);
    await expect(runJwt({ token: { sub: "authjs-sub", ...fields } })).resolves.toBeNull();
  });
});

describe("auth.config session callback", () => {
  it("exposes the roles and the user id", () => {
    const session = runSession({ session: { user: {} }, token: fields }) as {
      user: { roles: string[]; id?: string };
    };
    expect(session.user.roles).toEqual(["platform:admin"]);
    expect(session.user.id).toBe("user-1");
  });

  it("gives a session with no roles an empty list rather than undefined", () => {
    const session = runSession({ session: { user: {} }, token: {} }) as {
      user: { roles: string[] };
    };
    expect(session.user.roles).toEqual([]);
  });

  // The session object is serialized to the browser at /api/auth/session. A bearer
  // that reaches client JavaScript is one that can call the platform's API
  // directly, around the boundary the server actions exist to be.
  it("never puts the platform token on the session", () => {
    const session = runSession({ session: { user: {} }, token: fields });
    expect(JSON.stringify(session)).not.toContain("octo-token");
  });
});
