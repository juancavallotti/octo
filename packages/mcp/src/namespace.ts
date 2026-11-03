import type { RunHostPort } from "./run-host";

/**
 * Who is calling, as far as run isolation is concerned: the user and the client app
 * a verified bearer token names. Both are absent on a host that does not
 * authenticate its MCP endpoint.
 */
export interface McpCaller {
  /** The durable user id the host resolved the token to. */
  userId?: string;
  /** The OAuth client the token was issued to. */
  clientId?: string;
}

/** The parts of an MCP request's auth info that identify its caller. */
export interface AuthInfoLike {
  clientId?: string;
  extra?: Record<string, unknown>;
}

/** Resolves the run-host namespace for an MCP request, given its auth info. */
export type NamespaceResolver = (authInfo: AuthInfoLike | undefined) => string;

/**
 * Read the caller off an MCP request's auth info. The user id is what the host's
 * token verifier put on `AuthInfo.extra`; the client id is the standard
 * `AuthInfo.clientId`. Neither ever comes from tool arguments, so one client cannot
 * name another's runs.
 */
export function callerOf(authInfo: AuthInfoLike | undefined): McpCaller {
  const userId = authInfo?.extra?.userId;
  return {
    userId: typeof userId === "string" && userId !== "" ? userId : undefined,
    clientId: authInfo?.clientId ? authInfo.clientId : undefined,
  };
}

/**
 * Build a resolver that gives each caller its own run namespace, so concurrent
 * clients get isolated runners. MCP is served stateless, so there is no protocol
 * session to key on; an authenticated caller is keyed on (user, client) instead,
 * which also survives token refreshes and reconnects.
 *
 * An unauthenticated host — standalone and the desktop app — has no caller to tell
 * apart and shares one lazily-minted namespace. That host is local development, where
 * one MCP client drives one server.
 */
export function createNamespaceResolver(runHost: RunHostPort): NamespaceResolver {
  const byCaller = new Map<string, string>();
  let shared: string | null = null;
  return (authInfo) => {
    const { userId, clientId } = callerOf(authInfo);
    if (!userId) {
      if (!shared) shared = runHost.newNamespace();
      return shared;
    }
    const key = `${userId}\u0000${clientId ?? ""}`;
    let ns = byCaller.get(key);
    if (!ns) {
      ns = runHost.newNamespace();
      byCaller.set(key, ns);
    }
    return ns;
  };
}
