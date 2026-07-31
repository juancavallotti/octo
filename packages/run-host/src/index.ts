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
  invoke,
  evalCel,
  snapshot,
  subscribe,
  runningPort,
  type InvokeResult,
  type BreakOutcome,
  type DebugOutcome,
  type MockCase,
  type MockSpec,
  type SpyRecord,
  type SpyTrace,
  type LogLevel,
  type EvalResult,
  type RunResourceOptions,
} from "./session";
export {
  DEV_ENV_RESOURCE,
  type ResourceFile,
  type ResourceProvider,
} from "./resources";
export {
  probeVersion,
  cachedVersion,
  probeTestVersion,
  cachedTestVersion,
} from "./version";
export { probeSchema, cachedSchema } from "./schema";
export {
  test,
  type TestRunArgs,
  type TestRunOutcome,
  type TestSuiteInput,
  type TestSuiteResult,
  type TestCaseResult,
  type TestCaseOutcome,
  type TestCaseStatus,
  type TestFailure,
  type TestTotals,
} from "./test";
export {
  ensureNamespace,
  isValidNamespace,
  readNamespace,
  newNamespace,
  NAMESPACE_COOKIE,
  NAMESPACE_MAX_AGE_SECONDS,
} from "./namespace";
export { type LogLine } from "./logbuffer";
