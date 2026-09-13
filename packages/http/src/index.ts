/**
 * @octo/http — a tiny, framework-agnostic abstraction over `fetch` for server-side
 * clients. It turns a JSON request into a discriminated {@link ActionResult}, so callers
 * branch on a value instead of try/catch.
 *
 * It knows nothing about any specific service, auth, or URLs; callers pass a full URL and
 * build their own typed, domain-oriented client on top.
 */

export type { ActionResult, RequestOptions } from "./result";
export {
  requestBytes,
  requestJson,
  requestOk,
  requestStream,
  sendBytes,
} from "./request";
