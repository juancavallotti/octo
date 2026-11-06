import { ENVIRONMENT_HEADER, normalizeEnvironment } from "@/app/lib/environment";

/**
 * The environment an MCP connection acts in. An MCP client has no cookie and no
 * top bar, so it says so itself: the X-Octo-Environment header for a client
 * that can send headers, or `?environment=` on the endpoint's URL for one that
 * can only be given an address. Neither is the default environment, which is
 * what every connection meant before environments existed.
 *
 * Not validated here. Every call it makes is judged by the services, which
 * refuse an environment that does not exist, or one the caller holds nothing
 * in, with the same answer the platform gives anywhere else.
 */
export function mcpEnvironment(req: Request): string {
  const header = req.headers.get(ENVIRONMENT_HEADER);
  if (header) return normalizeEnvironment(header);
  try {
    return normalizeEnvironment(new URL(req.url).searchParams.get("environment"));
  } catch {
    return "";
  }
}
