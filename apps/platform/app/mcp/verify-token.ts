/**
 * Bearer-token verification for the `/mcp` resource server.
 *
 * One kind of bearer token is accepted: an **OAuth 2.1 access-token JWT**, which is
 * what MCP clients (Claude, ChatGPT) obtain by self-registering against the
 * operator's provider. It is verified against that provider's JWKS with
 * `iss`/`aud`/`exp` checks, and the `aud` must equal this server's RFC 8707 resource
 * identifier so a token minted for another resource can't be replayed here (MCP's
 * anti-passthrough rule).
 *
 * The token is then traded with iam for a platform token, which is what resolves
 * the caller to a durable octo user id and tells us their roles. Both, and the
 * platform token itself, are hung off {@link AuthInfo.extra} so the tools can
 * scope per-user work and carry the caller's own credential to the API.
 *
 * The signature check above is not made redundant by that exchange. iam checks
 * the same things, but MCP's anti-passthrough rule is about *this* resource
 * server refusing a token minted for somewhere else, and that check belongs
 * here.
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
import { exchangeIdToken } from "@/app/actions/client/iam";
import { OIDC_JWKS_URL, trimSlashes } from "@/oidc.config";
import { MCP_ISSUER, MCP_RESOURCE } from "./oauth-config";

/** Injectable collaborators, so tests need no network or live JWKS. */
export interface McpTokenVerifierDeps {
  /** Expected token issuer (`iss`) — the provider's issuer URL. */
  issuer: string;
  /** Expected token audience (`aud`) — this server's resource identifier. */
  resource: string;
  /** Resolve the signing key for a token; the provider's remote JWKS in production. */
  getKey: JWTVerifyGetKey;
  /** Trade the caller's provider token for a platform one. */
  exchangeIdToken: typeof exchangeIdToken;
}

/** A completed exchange, kept only as long as the platform token it holds. */
interface Exchanged {
  userId: string;
  roles: string[];
  octoToken: string;
  expiresAt: number;
}

/**
 * How long before a cached platform token expires it stops being reused. A minute
 * is far longer than a tool call takes, so nothing is handed a credential that
 * dies while it is being used.
 */
const EXCHANGE_LEAD_MS = 60_000;

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
  const exchanged = new Map<string, Exchanged>();

  /**
   * Trade the caller's token for a platform one, reusing the last result while it
   * is still good.
   *
   * Cached per subject rather than per token because a client refreshes its
   * access token far more often than the platform token behind it expires, and an
   * exchange is a round trip to iam plus one to the identity provider. The cached
   * entry is dropped a minute before its expiry so nothing is ever handed a
   * credential that dies mid-request.
   */
  async function exchange(subject: string, token: string): Promise<Exchanged | undefined> {
    const cached = exchanged.get(subject);
    if (cached && cached.expiresAt - Date.now() > EXCHANGE_LEAD_MS) return cached;

    const res = await deps.exchangeIdToken(token);
    if (!res.ok) {
      // No platform token means no identity we are willing to act on, so the
      // request is refused rather than admitted as an anonymous caller.
      console.warn(`could not exchange an MCP caller's token with iam: ${res.error}`);
      return undefined;
    }
    const fresh: Exchanged = {
      userId: res.data.user.id,
      roles: res.data.user.roles,
      octoToken: res.data.token,
      expiresAt: new Date(res.data.expiresAt).getTime(),
    };
    exchanged.set(subject, fresh);
    return fresh;
  }

  return async function verify(
    _req: Request,
    bearer?: string,
  ): Promise<AuthInfo | undefined> {
    if (!bearer) return undefined;

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

    const identity = await exchange(subject, bearer);
    if (!identity) return undefined;

    const scope = typeof payload.scope === "string" ? payload.scope : "";
    return {
      token: bearer,
      clientId: typeof payload.client_id === "string" ? payload.client_id : "",
      scopes: scope ? scope.split(" ").filter(Boolean) : [],
      expiresAt: typeof payload.exp === "number" ? payload.exp : undefined,
      resource: new URL(deps.resource),
      extra: {
        userId: identity.userId,
        subject,
        roles: identity.roles,
        // The caller's own credential, for the tools to spend against the API.
        octoToken: identity.octoToken,
      },
    };
  };
}

// --- Production wiring -----------------------------------------------------

/** The bits of the provider's OIDC discovery document we consume. */
interface Discovery {
  jwks_uri: string;
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

/** The verifier the `/mcp` route uses, bound to the configured provider and iam. */
export const verifyMcpToken: McpTokenVerifier = createMcpTokenVerifier({
  issuer: MCP_ISSUER,
  resource: MCP_RESOURCE,
  getKey: defaultGetKey,
  exchangeIdToken,
});
