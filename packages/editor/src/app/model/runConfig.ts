import YAML from "yaml";
import type { ConnectorInstance, EditorDocument } from "./document";
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
 * Serializes one config file of a project as the YAML stored for it.
 *
 * `service`, when given, is written as the file's `service.name`; it is given only
 * for the one file of the project that declares the identity, because the runtime
 * refuses a project in which two files do. `connectors` are the project's connections
 * (see {@link toConfig}).
 */
export function toFileYaml(
  doc: EditorDocument,
  opts: { service?: string; connectors?: ConnectorInstance[] } = {},
): string {
  const config = toConfig(doc, opts.connectors);
  const out =
    opts.service === undefined
      ? config
      : { service: { name: opts.service.trim() || RUN_SERVICE_NAME }, ...config };
  return YAML.stringify(out);
}

/**
 * Parses one config — a single file, or a project's files already merged — into an
 * editor document. The `service` block is ignored by {@link fromConfig}; to edit a
 * project's files, parse them together with `parseProject`.
 */
export function fromDefinitionYaml(definition: string): EditorDocument {
  const config = (YAML.parse(definition) ?? {}) as RuntimeConfig;
  return fromConfig(config);
}
