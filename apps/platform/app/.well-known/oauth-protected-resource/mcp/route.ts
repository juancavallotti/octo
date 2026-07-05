/**
 * Path-scoped protected-resource metadata for the `<origin>/mcp` resource
 * (RFC 9728 / MCP OAuth discovery). Implementation in app/mcp/protected-resource.
 */
export {
  protectedResourceGET as GET,
  protectedResourceOPTIONS as OPTIONS,
} from "@/app/mcp/protected-resource";

export const dynamic = "force-dynamic";
