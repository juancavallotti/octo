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
// How a dolphin suite is named and which flow it tests. Both hosts store suites
// differently — on disk beside the flows, or as orchestrator resources — but they must
// agree on this or a suite reads as missing in the tab that wrote it.
export {
  SUITE_SUFFIX,
  isSuiteFileName,
  suiteFileName,
  flowOfSuite,
} from "./app/suite/naming";
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
