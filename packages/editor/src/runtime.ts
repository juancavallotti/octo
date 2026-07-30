/**
 * Server-safe surface of the editor's document model — the pure helpers a host
 * needs to parse, validate, and inspect stored definitions outside the browser
 * (e.g. an MCP server), without dragging in the React editor components. Exposed
 * as the `@octo/editor/runtime` subpath so a Node route can import it cheaply.
 */

export {
  fromDefinitionYaml,
  toRunnableYaml,
  toDefinitionYaml,
} from "./app/model/runConfig";
// The dolphin test-suite model: how a suite is named, read, written, and how a case
// resolves against the file it lives in. Pure, so an MCP server can author a suite
// without the React editor — and shared, because both hosts and every consumer must
// agree with dolphin about what a suite means.
export {
  SUITE_SUFFIX,
  isSuiteFileName,
  suiteFileName,
  flowOfSuite,
} from "./app/suite/naming";
export {
  emptySuite,
  isSharedInput,
  isEmptyExpect,
  isValidDuration,
  outcomeOf,
  type Suite,
  type SuiteCase,
  type SuiteInput,
  type CaseInput,
  type Expectation,
  type MessageExpect,
  type RecordExpect,
  type SpyExpect,
  type Outcome,
} from "./app/suite/types";
export { parseSuite, type ParsedSuite } from "./app/suite/parse";
export { serializeSuite } from "./app/suite/serialize";
export { validateSuite, type SuiteIssue } from "./app/suite/validate";
export {
  addCase,
  removeCase,
  updateCase,
  nameTaken,
  uniqueCaseName,
} from "./app/suite/edit";
export {
  DEFAULT_CASE_TIMEOUT,
  inputFor,
  mocksFor,
  envFor,
  timeoutFor,
  spiedBy,
} from "./app/suite/merge";
export {
  validateDocument,
  issueMessages,
  type ValidationResult,
  type Issue,
} from "./app/model/validate";
// Capability schema + icon registry. resolveIcon returns icon *components* but
// only referencing them (not rendering) is server-safe, so a host can pick an
// icon for a stored definition without pulling in the React editor.
//
// `setCapabilities` is exported here for the same reason the editor entrypoint
// exports it: validateDocument checks block and connector types against the
// *active* catalogue, and the bundled one is an empty fallback. A server-side host
// that validates without injecting the runtime's schema first will find every type
// unknown — so a headless host (an MCP route) has to inject it just as the editor
// does. See docs/debug-seam.md and apps/*/app/mcp/route.ts.
export {
  CAPABILITIES,
  setCapabilities,
  resolveIcon,
  listConnectors,
  getConnectorSpec,
  getSourceSpec,
  type Capabilities,
  type ListedSource,
} from "./app/schema";
