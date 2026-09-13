import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

/**
 * What is worth testing here is the one decision this module makes: which cookie
 * to ask Auth.js for. It is invisible when wrong — a token that is not found
 * looks exactly like nobody being signed in — and it differs between a local run
 * and a deployed one, which is the combination that got it shipped broken.
 */

const getToken = vi.fn();
const headerBag = { "x-forwarded-proto": "https" } as Record<string, string>;

vi.mock("next-auth/jwt", () => ({ getToken: (args: unknown) => getToken(args) }));
vi.mock("next/headers", () => ({
  headers: async () => new Headers(headerBag),
}));

const load = async () => (await import("./sessionCookie")).currentOctoToken;

beforeEach(() => {
  vi.resetModules();
  getToken.mockReset().mockResolvedValue({ octoToken: "platform-token" });
  process.env.AUTH_SECRET = "secret";
  delete process.env.AUTH_URL;
  delete process.env.NEXTAUTH_URL;
});

afterEach(() => {
  delete process.env.AUTH_SECRET;
  delete process.env.AUTH_URL;
  delete process.env.NEXTAUTH_URL;
});

/** The flag the call was made with, which decides the cookie name and the salt. */
const secureCookieAsked = () => getToken.mock.calls[0][0].secureCookie;

describe("currentOctoToken", () => {
  it("asks for the prefixed cookie when the install is served over https", async () => {
    process.env.AUTH_URL = "https://octopaas.dev";
    await (await load())();
    expect(secureCookieAsked()).toBe(true);
  });

  it("asks for the plain cookie when the install is served over http", async () => {
    process.env.AUTH_URL = "http://localhost:3000";
    await (await load())();
    expect(secureCookieAsked()).toBe(false);
  });

  it("falls back to the forwarded protocol when no origin is configured", async () => {
    headerBag["x-forwarded-proto"] = "http";
    await (await load())();
    expect(secureCookieAsked()).toBe(false);
    headerBag["x-forwarded-proto"] = "https";
  });

  it("reads the token off the decrypted session", async () => {
    process.env.AUTH_URL = "https://octopaas.dev";
    await expect((await load())()).resolves.toBe("platform-token");
  });

  it("reports no token rather than throwing when the cookie will not decrypt", async () => {
    getToken.mockRejectedValue(new Error("decryption operation failed"));
    await expect((await load())()).resolves.toBeUndefined();
  });

  it("reports no token when the install has no AUTH_SECRET to decrypt with", async () => {
    delete process.env.AUTH_SECRET;
    await expect((await load())()).resolves.toBeUndefined();
    expect(getToken).not.toHaveBeenCalled();
  });
});
