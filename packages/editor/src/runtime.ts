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
export { validateDocument, type ValidationResult } from "./app/model/validate";
// Capability schema + icon registry. resolveIcon returns icon *components* but
// only referencing them (not rendering) is server-safe, so a host can pick an
// icon for a stored definition without pulling in the React editor.
export {
  CAPABILITIES,
  resolveIcon,
  listConnectors,
  getConnectorSpec,
  getSourceSpec,
  type ListedSource,
} from "./app/schema";
