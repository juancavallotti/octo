import YAML from "yaml";
import type { EditorDocument } from "./document";
import { fromConfig, toConfig, type RuntimeConfig } from "./serialize";

/**
 * Renders the editor document as the YAML the runtime loads, ready to write to
 * disk for `octo run -watch`. It reuses {@link toConfig} for the connectors/flows
 * mapping and prepends a `service` block so the runner has a name for its startup
 * banner and logs. Disk I/O lives server-side (the run API); this stays pure so it
 * can run in the browser and in tests.
 */

/** Service name stamped on configs the editor runs (purely cosmetic for logs). */
export const RUN_SERVICE_NAME = "octo-editor";

export function toRunnableYaml(doc: EditorDocument): string {
  const config = { service: { name: RUN_SERVICE_NAME }, ...toConfig(doc) };
  return YAML.stringify(config);
}

/**
 * Serializes the document as the YAML stored for a saved integration. It is the
 * same shape as {@link toRunnableYaml} but stamps the integration's own name in
 * the `service` block (falling back to {@link RUN_SERVICE_NAME} when unnamed) so
 * the runner's banner/logs identify the integration.
 */
export function toDefinitionYaml(doc: EditorDocument, name: string): string {
  const service = { name: name.trim() || RUN_SERVICE_NAME };
  const config = { service, ...toConfig(doc) };
  return YAML.stringify(config);
}

/**
 * Parses a stored integration definition back into an editor document. The
 * `service` block is cosmetic and ignored by {@link fromConfig}.
 */
export function fromDefinitionYaml(definition: string): EditorDocument {
  const config = (YAML.parse(definition) ?? {}) as RuntimeConfig;
  return fromConfig(config);
}
