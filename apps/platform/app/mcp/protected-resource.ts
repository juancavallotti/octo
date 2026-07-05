/**
 * OAuth 2.0 Protected Resource Metadata (RFC 9728) for the `/mcp` server.
 *
 * Served at both `/.well-known/oauth-protected-resource/mcp` (path-scoped, what
 * the MCP spec has clients derive from the `<origin>/mcp` resource) and the root
 * `/.well-known/oauth-protected-resource` as a fallback. Both advertise the same
 * `resource` identifier and point at eetr as the authorization server, so an MCP
 * client can discover eetr's `registration_endpoint` and run the OAuth flow.
 *
 * Returns 404 when OAuth isn't configured (local dev with SSO off), matching the
 * "no auth server to advertise" state.
 */

import {
  metadataCorsOptionsRequestHandler,
  protectedResourceHandler,
} from "mcp-handler";
import { MCP_ISSUER, MCP_RESOURCE, mcpOauthEnabled } from "./oauth-config";

const metadata = protectedResourceHandler({
  authServerUrls: [MCP_ISSUER],
  resourceUrl: MCP_RESOURCE,
});

/** Serve the metadata document, or 404 when OAuth is unconfigured. */
export function protectedResourceGET(
  request: Request,
): Response | Promise<Response> {
  if (!mcpOauthEnabled()) return new Response("Not found", { status: 404 });
  return metadata(request);
}

/** CORS preflight for browser-based MCP clients. */
export const protectedResourceOPTIONS = metadataCorsOptionsRequestHandler();
