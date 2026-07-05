/**
 * Root protected-resource metadata (RFC 9728), a fallback for MCP clients that
 * probe `/.well-known/oauth-protected-resource` instead of the path-scoped form.
 * Advertises the same `<origin>/mcp` resource. Implementation in
 * app/mcp/protected-resource.
 */
export {
  protectedResourceGET as GET,
  protectedResourceOPTIONS as OPTIONS,
} from "@/app/mcp/protected-resource";

export const dynamic = "force-dynamic";
