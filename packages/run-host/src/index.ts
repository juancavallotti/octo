/**
 * @octo/run-host — the server-side core of the editor's RUN feature, shared by
 * every app that hosts it (the platform's in-cluster runner and the standalone
 * app's local runner). It owns the `octo` child processes keyed by a per-user
 * namespace, buffers and streams their logs, allocates ports, and reaps idle
 * runners. Apps wrap these in their own thin Next route handlers (adding auth
 * where needed). Node-only — never import from a browser bundle.
 */

export {
  status,
  start,
  stop,
  sync,
  snapshot,
  subscribe,
  runningPort,
} from "./session";
export { probeVersion, cachedVersion } from "./version";
export {
  ensureNamespace,
  isValidNamespace,
  readNamespace,
  newNamespace,
  NAMESPACE_COOKIE,
  NAMESPACE_MAX_AGE_SECONDS,
} from "./namespace";
export { type LogLine } from "./logbuffer";
