import type { RunHostPort } from "./run-host";

/** Resolves the run-host namespace for an MCP request, given its session id. */
export type NamespaceResolver = (sessionId: string | undefined) => string;

/**
 * Build a resolver that gives each MCP session its own run namespace (so
 * concurrent clients get isolated runners, matching the editor's per-browser
 * cookie). The session→namespace map lives in the closure for the life of the
 * handler. Sessionless clients (stateless transport, no `Mcp-Session-Id`) share a
 * single lazily-minted namespace.
 */
export function createNamespaceResolver(runHost: RunHostPort): NamespaceResolver {
  const bySession = new Map<string, string>();
  let shared: string | null = null;
  return (sessionId) => {
    if (!sessionId) {
      if (!shared) shared = runHost.newNamespace();
      return shared;
    }
    let ns = bySession.get(sessionId);
    if (!ns) {
      ns = runHost.newNamespace();
      bySession.set(sessionId, ns);
    }
    return ns;
  };
}
