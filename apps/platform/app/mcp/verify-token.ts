/**
 * Bearer-token verification for the `/mcp` resource server.
 *
 * Two kinds of bearer token are accepted, chosen by prefix:
 *
 *  - **OAuth 2.1 access-token JWT** (the default for MCP clients like Claude and
 *    ChatGPT). Verified against the provider's JWKS with `iss`/`aud`/`exp` checks; the
 *    `aud` must equal this server's RFC 8707 resource identifier so a token minted
 *    for another resource can't be replayed here (MCP's anti-passthrough rule).
 *  - **`octo_…` API key** — the legacy per-user bearer, kept for a future CLI.
 *    Resolved through the orchestrator's verify endpoint, unchanged.
 *
 * Either way we resolve the caller to a durable octo user id and hang it off
 * {@link AuthInfo.extra} so tools can scope per-user work later; today it mirrors
 * the previous authentication-only boundary.
 *
 * The default export {@link verifyMcpToken} is wired to the configured provider's
 * real JWKS and the orchestrator; {@link createMcpTokenVerifier} takes injectable
 * deps for tests.
 */

import {
  createRemoteJWKSet,
  jwtVerify,
  type JWTPayload,
  type JWTVerifyGetKey,
} from "jose";
import type { AuthInfo } from "@modelcontextprotocol/sdk/server/auth/types.js";
import { bootstrapUser, verifyApiKey } from "@/app/actions/_client";
import { OIDC_JWKS_URL, OIDC_USERINFO_URL, trimSlashes } from "@/oidc.config";
import { MCP_ISSUER, MCP_RESOURCE } from "./oauth-config";

/** Claims we read from the authorization server's userinfo to provision a user. */
interface UserinfoClaims {
  email?: string;
  name?: string;
}

/** Injectable collaborators, so tests need no network or live JWKS. */
export interface McpTokenVerifierDeps {
  /** Expected token issuer (`iss`) — the provider's issuer URL. */
  issuer: string;
  /** Expected token audience (`aud`) — this server's resource identifier. */
  resource: string;
  /** Resolve the signing key for a token; the provider's remote JWKS in production. */
  getKey: JWTVerifyGetKey;
  /** Fetch email/name for the bearer's subject from the authorization server. */
  fetchUserinfo: (token: string) => Promise<UserinfoClaims>;
  /** Resolve an `octo_…` API key to its owner. */
  verifyApiKey: typeof verifyApiKey;
  /** Provision (or refresh) the octo user row for an OIDC subject. */
  bootstrapUser: typeof bootstrapUser;
}

/** The `verifyToken` callback shape `withMcpAuth` expects. */
export type McpTokenVerifier = (
  req: Request,
  bearer?: string,
) => Promise<AuthInfo | undefined>;

/**
 * Build a verifier over the given collaborators. The subject→userId cache lives
 * in the closure, so each verifier (and each test) is isolated.
 */
export function createMcpTokenVerifier(
  deps: McpTokenVerifierDeps,
): McpTokenVerifier {
  const userIdCache = new Map<string, string>();

  /** Resolve (and cache) the durable octo user id for an OIDC subject. */
  async function resolveUserId(
    subject: string,
    token: string,
  ): Promise<string | undefined> {
    const cached = userIdCache.get(subject);
    if (cached) return cached;
    // Access tokens carry only `sub`, so pull email/name from userinfo to seed
    // the user row (best-effort: a userinfo hiccup shouldn't fail the request).
    const claims = await deps
      .fetchUserinfo(token)
      .catch(() => ({}) as UserinfoClaims);
    const res = await deps.bootstrapUser(
      subject,
      claims.email ?? "",
      claims.name ?? "",
    );
    if (!res.ok) return undefined;
    userIdCache.set(subject, res.data.id);
    return res.data.id;
  }

  return async function verify(
    _req: Request,
    bearer?: string,
  ): Promise<AuthInfo | undefined> {
    if (!bearer) return undefined;

    // API key (kept for the future CLI): resolve via the orchestrator.
    if (bearer.startsWith("octo_")) {
      const res = await deps.verifyApiKey(bearer);
      if (!res.ok) return undefined;
      return {
        token: bearer,
        clientId: `apikey:${res.data.id}`,
        scopes: [],
        extra: { userId: res.data.userId },
      };
    }

    // OAuth 2.1 access-token JWT from the provider. Any failure (bad signature, wrong
    // issuer/audience, expiry) returns undefined; `withMcpAuth` then answers 401
    // with the `resource_metadata` pointer that starts the OAuth dance.
    let payload: JWTPayload;
    try {
      ({ payload } = await jwtVerify(bearer, deps.getKey, {
        issuer: deps.issuer,
        audience: deps.resource,
        algorithms: ["RS256"],
      }));
    } catch {
      return undefined;
    }

    const subject = payload.sub;
    if (!subject) return undefined;

    const userId = await resolveUserId(subject, bearer);
    const scope = typeof payload.scope === "string" ? payload.scope : "";
    return {
      token: bearer,
      clientId: typeof payload.client_id === "string" ? payload.client_id : "",
      scopes: scope ? scope.split(" ").filter(Boolean) : [],
      expiresAt: typeof payload.exp === "number" ? payload.exp : undefined,
      resource: new URL(deps.resource),
      extra: { userId, subject },
    };
  };
}

// --- Production wiring -----------------------------------------------------

/** The bits of the provider's OIDC discovery document we consume. */
interface Discovery {
  jwks_uri: string;
  userinfo_endpoint?: string;
}

/** Memoized OIDC discovery lookup (a failed fetch isn't cached). */
let discoveryPromise: Promise<Discovery> | null = null;
function discovery(): Promise<Discovery> {
  if (!discoveryPromise) {
    // trimSlashes here, not on the issuer itself: `iss` must be compared
    // against the issuer exactly as configured, but appending a path to one
    // that ends in a slash would ask for `//.well-known/…`.
    discoveryPromise = fetch(
      `${trimSlashes(MCP_ISSUER)}/.well-known/openid-configuration`,
    ).then((r) => {
      if (!r.ok) throw new Error(`OIDC discovery failed: ${r.status}`);
      return r.json() as Promise<Discovery>;
    });
    discoveryPromise.catch(() => {
      discoveryPromise = null;
    });
  }
  return discoveryPromise;
}

/**
 * Lazily-built remote JWKS, keyed off `OIDC_JWKS_URL` when the operator pinned
 * one and off the discovered `jwks_uri` otherwise.
 */
let remoteJwks: JWTVerifyGetKey | null = null;
const defaultGetKey: JWTVerifyGetKey = async (header, input) => {
  if (!remoteJwks) {
    const jwksUrl = OIDC_JWKS_URL ?? (await discovery()).jwks_uri;
    remoteJwks = createRemoteJWKSet(new URL(jwksUrl));
  }
  return remoteJwks(header, input);
};

/**
 * Fetch email/name from the provider's userinfo endpoint with the caller's
 * token. Skipped entirely when neither the override nor discovery names one —
 * userinfo is optional, and a provider without it simply yields no claims.
 */
async function defaultFetchUserinfo(token: string): Promise<UserinfoClaims> {
  const endpoint = OIDC_USERINFO_URL ?? (await discovery()).userinfo_endpoint;
  if (!endpoint) return {};
  const res = await fetch(endpoint, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) return {};
  const u = (await res.json()) as Record<string, unknown>;
  return {
    email: typeof u.email === "string" ? u.email : undefined,
    name: typeof u.name === "string" ? u.name : undefined,
  };
}

/** The verifier the `/mcp` route uses, bound to the configured provider + the orchestrator. */
export const verifyMcpToken: McpTokenVerifier = createMcpTokenVerifier({
  issuer: MCP_ISSUER,
  resource: MCP_RESOURCE,
  getKey: defaultGetKey,
  fetchUserinfo: defaultFetchUserinfo,
  verifyApiKey,
  bootstrapUser,
});
