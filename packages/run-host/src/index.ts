/**
 * @octo/run-host — the server-side core of the editor's RUN feature, shared by
 * every app that hosts it. It comes in two halves, and the split is the shape of
 * the package:
 *
 *  - **The app runner** (`runner.ts`) is the long-running thing: the app the editor
 *    started, its logs, its address. It has a backend — a local child process of
 *    this pod, or a dev-run pod the orchestrator manages — behind one interface.
 *  - **The one-shots** (`exec/`) are `invoke`, `evalCel` and `test`: spawn a child,
 *    wait, report. They hold no process, port or buffer, so they need no backend
 *    and stay plain functions.
 *
 * Apps wrap these in their own thin Next route handlers and server actions (adding
 * auth where needed). Node-only — never import from a browser bundle.
 */

export {
  type AppRunner,
  type LogStreamOptions,
  type RunKey,
  type RunState,
  type StartArgs,
  type SyncArgs,
} from "./runner";
export {
  status,
  start,
  stop,
  sync,
  snapshot,
  subscribe,
  runningPort,
  localRunner,
} from "./runner/local";
export { type RunResourceOptions } from "./staging";
export {
  invoke,
  type InvokeResult,
  type BreakOutcome,
  type DebugOutcome,
  type MockCase,
  type MockSpec,
  type SpyRecord,
  type SpyTrace,
  type LogLevel,
} from "./exec/invoke";
export { evalCel, type EvalResult } from "./exec/eval";
export {
  DEV_ENV_RESOURCE,
  type ResourceFile,
  type ResourceProvider,
} from "./resources";
export {
  binaries,
  probeVersion,
  cachedVersion,
  probeTestVersion,
  cachedTestVersion,
  type Binaries,
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
} from "./exec/test";
export {
  deriveNamespace,
  ensureNamespace,
  isValidNamespace,
  isValidTabId,
  readNamespace,
  newNamespace,
  NAMESPACE_COOKIE,
  NAMESPACE_MAX_AGE_SECONDS,
} from "./namespace";
export { type LogLine } from "./logbuffer";
