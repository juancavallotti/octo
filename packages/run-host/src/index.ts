/**
 * @octo/run-host — what the two hosts of the editor's RUN feature genuinely share.
 * The package is shaped by what that turns out to be, and by what it is not:
 *
 *  - **The app-runner port** (`runner.ts`) — the long-running app's interface, with no
 *    implementation. The two backends are a child process of the host and a dev-run pod
 *    somewhere else, and each belongs to the app that runs that way. A shared interface
 *    with app-owned implementations is the point; a shared implementation that one app
 *    ignores would be a dependency pointing the wrong way.
 *  - **The one-shots** (`exec/`) — `invoke`, `evalCel` and `test`: spawn a child, wait,
 *    report. Both hosts run them identically and locally, which is what makes them
 *    shared rather than merely co-located.
 *  - **The pieces both of those need**: staging a config and its resources, finding the
 *    binaries, the runtime's capability schema, and the run namespace.
 *
 * Apps wrap these in their own thin Next route handlers and server actions (adding
 * auth where needed). Node-only — never import from a browser bundle.
 */

export {
  type AppRunner,
  type LogLine,
  type LogStreamOptions,
  type RunKey,
  type RunState,
  type StartArgs,
  type SyncArgs,
} from "./runner";
export { namespaceDir, writeConfig, type RunResourceOptions } from "./staging";
export { octoBin, terminate } from "./child";
export {
  DEV_ENV_RESOURCE,
  effectiveResourceNames,
  injectDevEnvResource,
  resolveAndStage,
  sameNameSet,
  stageResources,
  type ResourceFile,
  type ResourceProvider,
} from "./resources";
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
