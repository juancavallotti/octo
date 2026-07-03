import type { ResourceFile, ResourceProvider } from "@octo/run-host";
import type { Resource } from "@/app/model/orchestrator";
import * as client from "@/app/actions/_client";

/**
 * Platform resource provider for editor/MCP runs. An integration's resources
 * (env files, templates) live in the orchestrator DB; here we fetch them for the
 * names a run declares so @octo/run-host can stage them into the run's namespace
 * dir. Reached through the same typed client the server actions use — auth is the
 * caller's concern (OIDC-gated actions for the editor, API key for the MCP route).
 */

/** Keep only the declared resources and reduce them to what run-host stages. */
export function toResourceFiles(all: Resource[], names: string[]): ResourceFile[] {
  const wanted = new Set(names);
  return all
    .filter((r) => wanted.has(r.name))
    .map((r) => ({ name: r.name, content: r.content }));
}

/**
 * Build a {@link ResourceProvider} bound to one integration. It lists the
 * integration's resources once per call and returns those the run declared; a
 * failed list surfaces as a start failure via the caller's error handling.
 */
export function orchestratorResourceProvider(integrationId: string): ResourceProvider {
  return async (names) => {
    if (names.length === 0) return [];
    const res = await client.listResources(integrationId);
    if (!res.ok) throw new Error(res.error);
    return toResourceFiles(res.data, names);
  };
}
