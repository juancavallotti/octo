/**
 * OAuth 2.0 Protected Resource Metadata (RFC 9728) for the `/mcp` server.
 *
 * Served at both `/.well-known/oauth-protected-resource/mcp` (path-scoped, what
 * the MCP spec has clients derive from the `<origin>/mcp` resource) and the root
 * `/.well-known/oauth-protected-resource` as a fallback. Both advertise the same
 * `resource` identifier and point at the configured OIDC provider as the authorization
 * server, so an MCP client can discover its `registration_endpoint` and run the flow.
 *
 * They also name the scopes a client should request. A client takes its scopes from
 * here, and one that finds none asks for none — which gets the provider's default
 * token, without the `email` iam needs to resolve the caller to a user. So these are
 * the scopes the platform's own sign-in requests: both tokens are traded with iam the
 * same way.
 *
 * Returns 404 when this install has no public origin to name itself by (no
 * AUTH_URL and no MCP_RESOURCE_URL), matching the "nothing to advertise" state.
 */

import {
  generateProtectedResourceMetadata,
  metadataCorsOptionsRequestHandler,
} from "mcp-handler";
import { OIDC_SCOPES } from "@/oidc.config";
import { MCP_ISSUER, MCP_RESOURCE } from "./oauth-config";

/** What the metadata document describes. */
export interface ProtectedResourceConfig {
  /** The authorization server's issuer URL; empty when OAuth is unconfigured. */
  issuer: string;
  /** This server's resource identifier; empty when there is no public origin. */
  resource: string;
  /** Space-separated scopes a client should request. */
  scopes: string;
}

/** The CORS headers the metadata routes answer with, so browser clients can read them. */
const CORS_HEADERS = {
  "Access-Control-Allow-Origin": "*",
  "Access-Control-Allow-Methods": "GET, OPTIONS",
  "Access-Control-Allow-Headers": "*",
  "Access-Control-Max-Age": "86400",
};

/** Build the GET handler for the metadata document described by `config`. */
export function createProtectedResourceGET(
  config: ProtectedResourceConfig,
): (request: Request) => Response {
  if (!config.issuer || !config.resource) {
    return () => new Response("Not found", { status: 404 });
  }
  const body = JSON.stringify(
    generateProtectedResourceMetadata({
      authServerUrls: [config.issuer],
      resourceUrl: config.resource,
      additionalMetadata: {
        scopes_supported: config.scopes.split(/\s+/).filter(Boolean),
      },
    }),
  );
  return () =>
    new Response(body, {
      headers: {
        ...CORS_HEADERS,
        "Cache-Control": "max-age=3600",
        "Content-Type": "application/json",
      },
    });
}

/** Serve the metadata document, or 404 when OAuth is unconfigured. */
export const protectedResourceGET = createProtectedResourceGET({
  issuer: MCP_ISSUER,
  resource: MCP_RESOURCE,
  scopes: OIDC_SCOPES,
});

/** CORS preflight for browser-based MCP clients. */
export const protectedResourceOPTIONS = metadataCorsOptionsRequestHandler();
