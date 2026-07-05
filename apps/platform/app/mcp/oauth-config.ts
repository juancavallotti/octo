/**
 * Shared OAuth 2.1 configuration for the `/mcp` resource server.
 *
 * The platform is an OAuth 2.1 *resource server*: it does not issue tokens. It
 * trusts eetr (`auth.eetr.app`) as the authorization server — the same OIDC
 * provider that gates the editor (see auth.config.ts) — and validates the bearer
 * access-token JWTs eetr mints for MCP clients (Claude, ChatGPT, …).
 *
 * These constants are consumed by:
 *  - the `/mcp` route's {@link ../mcp/verify-token} (issuer + resource → aud check),
 *  - the `.well-known/oauth-protected-resource` metadata routes (RFC 9728), which
 *    point clients at the authorization server so they can self-register (DCR).
 *
 * They are intentionally free of the `jose`/orchestrator imports the verifier
 * pulls in, so the metadata routes stay light.
 */

/** Trim any trailing slashes so we can safely append a path. */
function trimSlashes(value: string): string {
  return value.replace(/\/+$/, "");
}

/**
 * The authorization server we trust — eetr's issuer, reused from the editor's
 * OIDC config. Access tokens must carry `iss` equal to this. Empty when SSO is
 * unconfigured (local dev), in which case MCP OAuth is effectively disabled.
 */
export const MCP_ISSUER = trimSlashes(process.env.AUTH_EETR_ISSUER ?? "");

/**
 * The public origin of this deployment (Auth.js's canonical var). Used as the
 * origin for the `resource_metadata` URL in the 401 challenge.
 */
export const MCP_ORIGIN = trimSlashes(process.env.AUTH_URL ?? "");

/**
 * The RFC 8707 resource identifier for this MCP server — the value clients pass
 * as `resource` and eetr stamps into the access token's `aud`. It is the public
 * `/mcp` URL. `MCP_RESOURCE_URL` overrides it for proxies/custom hosts.
 */
export const MCP_RESOURCE =
  trimSlashes(process.env.MCP_RESOURCE_URL ?? "") ||
  (MCP_ORIGIN ? `${MCP_ORIGIN}/mcp` : "");

/**
 * Path of the protected-resource metadata document. Path-scoped per the MCP spec
 * (for resource `<origin>/mcp`, clients look under `.../oauth-protected-resource/mcp`);
 * the root document is also served as a fallback.
 */
export const RESOURCE_METADATA_PATH =
  "/.well-known/oauth-protected-resource/mcp";

/** Whether MCP OAuth is configured (issuer + resource both known). */
export const mcpOauthEnabled = (): boolean => !!MCP_ISSUER && !!MCP_RESOURCE;
