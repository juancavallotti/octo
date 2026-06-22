import { getConnectorSpec } from "@/app/schema";
import { ConnectorInstance, EditorDocument, SourceNode, newId } from "./document";
import { uniqueSlug } from "./identity";

/**
 * Helpers for document-global connector instances ("connections"). Kept apart
 * from document.ts so each model file stays small (see
 * docs/editor-coding-standards.md). A connection is referenced by its unique,
 * slug-style `name` from a flow's source and from block settings.
 */

/** Seed a connector instance's settings from its schema field defaults. */
export function defaultConnectorSettings(
  type: string,
): Record<string, unknown> {
  const spec = getConnectorSpec(type);
  if (!spec) return {};
  const settings: Record<string, unknown> = {};
  for (const field of spec.settings) {
    if (field.default !== undefined) settings[field.name] = field.default;
  }
  return settings;
}

/** Create a fresh connector instance with a unique, slug-style name. */
export function newConnector(
  type: string,
  taken: Set<string>,
): ConnectorInstance {
  return {
    id: newId(),
    name: uniqueSlug(type, taken),
    type,
    settings: defaultConnectorSettings(type),
  };
}

/**
 * Resolves the connector instance name the runtime should see for a source. The
 * runtime matches strictly by name, so: an explicit binding wins; otherwise a
 * lone connection of the matching type binds implicitly (so the common 0–1
 * connector case "just works"); otherwise we fall back to the type name.
 */
export type ConnectorResolver = (source: SourceNode) => string | undefined;

export function connectorResolver(
  connectors: ConnectorInstance[],
): ConnectorResolver {
  const byType = new Map<string, string[]>();
  for (const c of connectors) {
    if (!c.name) continue;
    const names = byType.get(c.type);
    if (names) names.push(c.name);
    else byType.set(c.type, [c.name]);
  }
  return (source) => {
    if (source.connectorRef) return source.connectorRef;
    const ofType = byType.get(source.connector ?? "");
    if (ofType && ofType.length === 1) return ofType[0];
    return source.connector || undefined;
  };
}

/** Find a connector instance by id. */
export function findConnector(
  doc: EditorDocument,
  id: string,
): ConnectorInstance | undefined {
  return doc.connectors.find((c) => c.id === id);
}
