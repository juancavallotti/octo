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
function makeVerifier(over: Partial<McpTokenVerifierDeps> = {}) {
  const bootstrapUser = vi.fn(async () => ({
    ok: true as const,
    data: { id: "octo-user-1", email: "", name: "", createdAt: "", lastLoginAt: "" },
  }));
  const verifyApiKey = vi.fn(async () => ({
    ok: true as const,
    data: { id: "key-1", userId: "octo-user-9", name: "cli" },
  }));
  const fetchUserinfo = vi.fn(async () => ({ email: "a@b.co", name: "Ada" }));
  const deps: McpTokenVerifierDeps = {
    issuer: ISSUER,
    resource: RESOURCE,
    getKey,
    fetchUserinfo,
    verifyApiKey: verifyApiKey as unknown as McpTokenVerifierDeps["verifyApiKey"],
    bootstrapUser: bootstrapUser as unknown as McpTokenVerifierDeps["bootstrapUser"],
    ...over,
  };
  return { verify: createMcpTokenVerifier(deps), bootstrapUser, verifyApiKey, fetchUserinfo };
}

const req = new Request("https://platform.example/mcp");

describe("verifyMcpToken — OAuth JWT", () => {
  it("accepts a valid token and resolves the user via userinfo + bootstrap", async () => {
    const { verify, bootstrapUser, fetchUserinfo } = makeVerifier();
    const info = await verify(req, await mintToken());

    expect(info).toBeDefined();
    expect(info!.extra).toMatchObject({ userId: "octo-user-1", subject: "user-sub-1" });
    expect(info!.clientId).toBe("cid-123");
    expect(info!.scopes).toEqual(["openid", "profile", "email"]);
    expect(info!.resource?.toString()).toBe(RESOURCE);
    // userinfo claims seed the bootstrap call.
    expect(fetchUserinfo).toHaveBeenCalledOnce();
    expect(bootstrapUser).toHaveBeenCalledWith("user-sub-1", "a@b.co", "Ada");
  });

  it("caches subject→userId (bootstrap runs once across requests)", async () => {
    const { verify, bootstrapUser } = makeVerifier();
    await verify(req, await mintToken());
    await verify(req, await mintToken());
    expect(bootstrapUser).toHaveBeenCalledOnce();
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

  it("still authenticates when the user can't be provisioned (userId undefined)", async () => {
    const bootstrapUser = vi.fn(async () => ({ ok: false as const, error: "orchestrator down" }));
    const { verify } = makeVerifier({
      bootstrapUser: bootstrapUser as unknown as McpTokenVerifierDeps["bootstrapUser"],
    });
    const info = await verify(req, await mintToken());
    expect(info).toBeDefined();
    expect(info!.extra?.userId).toBeUndefined();
  });
});

describe("verifyMcpToken — octo_ API key", () => {
  it("resolves a valid API key via the orchestrator", async () => {
    const { verify, verifyApiKey } = makeVerifier();
    const info = await verify(req, "octo_abc123");
    expect(verifyApiKey).toHaveBeenCalledWith("octo_abc123");
    expect(info).toMatchObject({ clientId: "apikey:key-1", extra: { userId: "octo-user-9" } });
  });

  it("rejects an unknown/expired API key", async () => {
    const verifyApiKey = vi.fn(async () => ({ ok: false as const, error: "revoked" }));
    const { verify } = makeVerifier({
      verifyApiKey: verifyApiKey as unknown as McpTokenVerifierDeps["verifyApiKey"],
    });
    expect(await verify(req, "octo_bad")).toBeUndefined();
  });
});

describe("verifyMcpToken — no credentials", () => {
  it("returns undefined when no bearer is present", async () => {
    const { verify } = makeVerifier();
    expect(await verify(req, undefined)).toBeUndefined();
  });
});
