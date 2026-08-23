/**
 * Shared OAuth 2.1 configuration for the `/mcp` resource server.
 *
 * The platform is an OAuth 2.1 *resource server*: it does not issue tokens. It
 * trusts the operator's configured OIDC provider as the authorization server —
 * the same one that gates the editor (see oidc.config.ts) — and validates the
 * bearer access-token JWTs it mints for MCP clients (Claude, ChatGPT, …).
 *
 * These constants are consumed by:
 *  - the `/mcp` route's {@link ../mcp/verify-token} (issuer + resource → aud check),
 *  - the `.well-known/oauth-protected-resource` metadata routes (RFC 9728), which
 *    point clients at the authorization server so they can self-register (DCR).
 *
 * They are intentionally free of the `jose`/orchestrator imports the verifier
 * pulls in, so the metadata routes stay light.
 */

import { OIDC_ISSUER, trimSlashes } from "@/oidc.config";

/**
 * The authorization server we trust — the provider's issuer, reused as-is from
 * the editor's OIDC config so the two can never disagree. Access tokens must
 * carry `iss` equal to this. Empty when SSO is unconfigured (local dev), in
 * which case MCP OAuth is effectively disabled.
 */
export const MCP_ISSUER = OIDC_ISSUER;

/**
 * The public origin of this deployment (Auth.js's canonical var). Used as the
 * origin for the `resource_metadata` URL in the 401 challenge.
 */
export const MCP_ORIGIN = trimSlashes(process.env.AUTH_URL ?? "");

/**
 * The RFC 8707 resource identifier for this MCP server — the value clients pass
 * as `resource` and the provider stamps into the access token's `aud`. It is the public
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
