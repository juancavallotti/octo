/**
 * The orchestrator client: a typed, domain-oriented API with one named function
 * per operation, and deliberately no HTTP verbs — paths, methods, JSON encoding
 * and the server-only ORCHESTRATOR_URL all stay inside `client/http.ts`. It is
 * auth-agnostic; authorization is applied by the calling action (`_auth.ts`).
 *
 *     serverAction (auth) → this client (listFolders(), …) → requestJson() → fetch
 *
 * Every function returns a discriminated {@link ActionResult} (server actions
 * cannot throw readable errors in production); the model layer unwraps it.
 *
 * This file is the barrel. The operations live in `client/`, grouped the way the
 * banners here already grouped them, so no caller's import has to change.
 */

export type { ActionResult } from "@octo/http";

export * from "./client/identity";
export * from "./client/integrations";
export * from "./client/deployments";
