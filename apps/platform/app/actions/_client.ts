/**
 * The orchestrator client: a typed, domain-oriented API with one named function
 * per operation and no HTTP verbs — paths, methods, JSON encoding and the
 * server-only ORCHESTRATOR_URL all stay inside `client/http.ts`. It is
 * auth-agnostic; authorization is applied by the calling action (`_auth.ts`).
 *
 *     serverAction (auth) → this client (listFolders(), …) → requestJson() → fetch
 *
 * Every function returns a discriminated {@link ActionResult}, since server
 * actions cannot throw readable errors in production.
 *
 * This file is the barrel; the operations live in `client/`.
 */

export type { ActionResult } from "@octo/http";

export * from "./client/identity";
export * from "./client/integrations";
export * from "./client/bundles";
export * from "./client/deployments";
export * from "./client/settings";
export * from "./client/agent";
