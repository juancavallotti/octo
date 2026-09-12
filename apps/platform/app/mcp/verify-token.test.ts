// @vitest-environment node
// verify-token is server-only; jose's WebCrypto signing needs Node's Uint8Array
// realm (jsdom's breaks `instanceof` checks during SignJWT).
import { describe, it, expect, vi, beforeEach } from "vitest";
import { SignJWT, generateKeyPair, exportJWK, type JWTVerifyGetKey } from "jose";
import {
  createMcpTokenVerifier,
  type McpTokenVerifierDeps,
} from "./verify-token";

const ISSUER = "https://idp.example.com";
const RESOURCE = "https://platform.example/mcp";

/** A fresh RS256 keypair per suite; `getKey` returns the matching public key. */
let privateKey: CryptoKey;
let publicKey: CryptoKey;
let getKey: JWTVerifyGetKey;

beforeEach(async () => {
  const pair = await generateKeyPair("RS256");
  privateKey = pair.privateKey;
  publicKey = pair.publicKey;
  // exportJWK round-trip keeps the fake close to the remote-JWKS behavior.
  await exportJWK(publicKey);
  getKey = (async () => publicKey) as unknown as JWTVerifyGetKey;
});

/** Mint an access-token JWT, overriding any registered claim/header. */
async function mintToken(
  claims: Record<string, unknown> = {},
  opts: { issuer?: string; audience?: string; exped?: string; key?: CryptoKey } = {},
): Promise<string> {
  return new SignJWT({ scope: "openid profile email", client_id: "cid-123", ...claims })
    .setProtectedHeader({ alg: "RS256", kid: "test" })
    .setIssuer(opts.issuer ?? ISSUER)
    .setSubject((claims.sub as string) ?? "user-sub-1")
    .setAudience(opts.audience ?? RESOURCE)
    .setIssuedAt()
    .setExpirationTime(opts.exped ?? "5m")
    .sign(opts.key ?? privateKey);
}

/** Build a verifier over fakes; individual specs tweak the returned mocks. */
/** What iam answers with for a caller it recognises. */
function minted(over: { roles?: string[]; expiresAt?: string } = {}) {
  return {
    ok: true as const,
    data: {
      token: "octo-token",
      expiresAt: over.expiresAt ?? new Date(Date.now() + 3600_000).toISOString(),
      user: {
        id: "octo-user-1",
        email: "a@b.co",
        name: "Ada",
        roles: over.roles ?? ["platform:developer"],
        createdAt: "",
        lastLoginAt: "",
      },
    },
  };
}

function makeVerifier(over: Partial<McpTokenVerifierDeps> = {}) {
  const exchangeIdToken = vi.fn(async () => minted());
  const deps: McpTokenVerifierDeps = {
    issuer: ISSUER,
    resource: RESOURCE,
    getKey,
    exchangeIdToken: exchangeIdToken as unknown as McpTokenVerifierDeps["exchangeIdToken"],
    ...over,
  };
  return { verify: createMcpTokenVerifier(deps), exchangeIdToken };
}

const req = new Request("https://platform.example/mcp");

describe("verifyMcpToken — OAuth JWT", () => {
  it("accepts a valid token and resolves the caller by exchanging it with iam", async () => {
    const { verify, exchangeIdToken } = makeVerifier();
    const token = await mintToken();
    const info = await verify(req, token);

    expect(info).toBeDefined();
    expect(info!.extra).toMatchObject({
      userId: "octo-user-1",
      subject: "user-sub-1",
      roles: ["platform:developer"],
      // The caller's own credential, for the tools to spend against the API.
      octoToken: "octo-token",
    });
    expect(info!.clientId).toBe("cid-123");
    expect(info!.scopes).toEqual(["openid", "profile", "email"]);
    expect(info!.resource?.toString()).toBe(RESOURCE);
    expect(exchangeIdToken).toHaveBeenCalledWith(token);
  });

  it("reuses the exchange while the platform token it returned is still good", async () => {
    const { verify, exchangeIdToken } = makeVerifier();
    await verify(req, await mintToken());
    await verify(req, await mintToken());
    expect(exchangeIdToken).toHaveBeenCalledOnce();
  });

  // A client refreshes its access token far more often than the platform token
  // behind it expires, but the cached one must not outlive its own expiry.
  it("exchanges again once the cached platform token is near expiry", async () => {
    const exchangeIdToken = vi.fn(async () =>
      minted({ expiresAt: new Date(Date.now() + 30_000).toISOString() }),
    );
    const { verify } = makeVerifier({
      exchangeIdToken: exchangeIdToken as unknown as McpTokenVerifierDeps["exchangeIdToken"],
    });
    await verify(req, await mintToken());
    await verify(req, await mintToken());
    expect(exchangeIdToken).toHaveBeenCalledTimes(2);
  });

  it("rejects an expired token", async () => {
    const { verify } = makeVerifier();
    expect(await verify(req, await mintToken({}, { exped: "-1m" }))).toBeUndefined();
  });

  it("rejects a wrong audience (anti-passthrough)", async () => {
    const { verify } = makeVerifier();
    const token = await mintToken({}, { audience: "https://evil.example/mcp" });
    expect(await verify(req, token)).toBeUndefined();
  });

  it("rejects a wrong issuer", async () => {
    const { verify } = makeVerifier();
    const token = await mintToken({}, { issuer: "https://evil.example" });
    expect(await verify(req, token)).toBeUndefined();
  });

  it("rejects a bad signature (token signed by another key)", async () => {
    const { verify } = makeVerifier();
    const other = await generateKeyPair("RS256");
    const token = await mintToken({}, { key: other.privateKey });
    expect(await verify(req, token)).toBeUndefined();
  });

  // A change of posture from when this only had to authenticate: the tools now
  // carry the caller's own credential, so a caller iam will not vouch for is
  // refused rather than admitted with no identity attached.
  it("refuses a caller iam will not exchange for", async () => {
    const exchangeIdToken = vi.fn(async () => ({ ok: false as const, error: "iam down" }));
    const { verify } = makeVerifier({
      exchangeIdToken: exchangeIdToken as unknown as McpTokenVerifierDeps["exchangeIdToken"],
    });
    expect(await verify(req, await mintToken())).toBeUndefined();
  });
});

describe("verifyMcpToken — no credentials", () => {
  it("returns undefined when no bearer is present", async () => {
    const { verify } = makeVerifier();
    expect(await verify(req, undefined)).toBeUndefined();
  });
});
