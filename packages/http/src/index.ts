/**
 * @octo/http — a tiny, framework-agnostic abstraction over `fetch`, shared by the
 * apps' server-side clients (e.g. the platform's orchestrator client and the
 * standalone app's local clients). It turns a JSON request into a discriminated
 * {@link ActionResult} so callers branch on a value instead of try/catch — which
 * is what Next.js server actions need.
 *
 * It knows nothing about any specific service, auth, or URLs; callers pass a full
 * URL and build their own typed, domain-oriented client on top.
 */

export type { ActionResult } from "./result";
export { requestJson, requestOk } from "./request";
