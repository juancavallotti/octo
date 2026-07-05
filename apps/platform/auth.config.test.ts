import { beforeEach, describe, expect, it, vi } from "vitest";

// The jwt callback provisions the user through the orchestrator client; stub it so
// we can assert *which* subject value it is handed, with no network in play.
const { bootstrapUser } = vi.hoisted(() => ({
  bootstrapUser: vi.fn(async () => ({ ok: true, data: { id: "user-1" } })),
}));
vi.mock("@/app/actions/_client", () => ({ bootstrapUser }));

import { authConfig } from "./auth.config";

/** Invoke the jwt callback with only the fields it actually reads. */
function runJwt(args: {
  token: Record<string, unknown>;
  profile: Record<string, unknown>;
}) {
  // The real signature carries more params (account, user, …) that this callback
  // ignores; cast so the test can pass just token + profile.
  return (authConfig.callbacks!.jwt as (a: unknown) => Promise<unknown>)(args);
}

describe("auth.config jwt callback", () => {
  beforeEach(() => {
    bootstrapUser.mockClear();
  });

  it("keys the user on the real OIDC sub, not Auth.js's minted token.sub", async () => {
    // Auth.js mints a fresh token.sub per sign-in; the IdP's profile.sub is stable.
    await runJwt({
      token: { sub: "authjs-minted-per-signin" },
      profile: { sub: "real-oidc-sub", email: "user@example.com", name: "User" },
    });

    expect(bootstrapUser).toHaveBeenCalledWith(
      "real-oidc-sub",
      "user@example.com",
      "User",
    );
    // Regression guard: the old code preferred token.sub, spawning a new user row
    // every sign-in.
    expect(bootstrapUser).not.toHaveBeenCalledWith(
      "authjs-minted-per-signin",
      expect.anything(),
      expect.anything(),
    );
  });

  it("falls back to token.sub only when the profile carries no sub", async () => {
    await runJwt({
      token: { sub: "fallback-sub" },
      profile: { email: "user@example.com" },
    });

    expect(bootstrapUser).toHaveBeenCalledWith(
      "fallback-sub",
      "user@example.com",
      "",
    );
  });
});
