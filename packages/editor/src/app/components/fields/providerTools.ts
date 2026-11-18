import type { EditorDocument } from "../../model/document";
import { getConnectorSpec } from "../../schema";
import type { ProviderToolSpec } from "../../schema/types";

/** One enabled provider tool, as it serializes under the block's settings. */
export type ProviderToolEntry = {
  type: string;
  settings?: Record<string, unknown>;
};

/** Reads a stored `provider-tool-list` value, tolerating anything malformed. */
export function toEntries(value: unknown): ProviderToolEntry[] {
  if (!Array.isArray(value)) return [];
  return value
    .filter((v): v is Record<string, unknown> => !!v && typeof v === "object")
    .map((v) => ({
      type: typeof v.type === "string" ? v.type : "",
      settings:
        v.settings && typeof v.settings === "object"
          ? { ...(v.settings as Record<string, unknown>) }
          : undefined,
    }));
}

/**
 * Renders entries for storage: settings that were cleared are dropped, and an
 * entry left with none carries no `settings` key at all, so a bare tool reads
 * back as `{type}`.
 */
export function serializeEntries(
  entries: ProviderToolEntry[],
): ProviderToolEntry[] {
  return entries.map(({ type, settings }) => {
    const kept = Object.fromEntries(
      Object.entries(settings ?? {}).filter(
        ([, v]) => v !== undefined && v !== "" && !(Array.isArray(v) && v.length === 0),
      ),
    );
    return Object.keys(kept).length > 0 ? { type, settings: kept } : { type };
  });
}

/**
 * The catalogue of the connection a block names, or undefined when it names none
 * the editor can resolve to a known connector type — unset, missing from the
 * project, or an environment placeholder whose type is only known at runtime.
 */
export function catalogueFor(
  doc: EditorDocument,
  connection: unknown,
): ProviderToolSpec[] | undefined {
  if (typeof connection !== "string" || connection === "") return undefined;
  const declared = doc.connectors.find((c) => c.name === connection);
  const spec = declared ? getConnectorSpec(declared.type) : undefined;
  if (!spec) return undefined;
  return spec.providerTools ?? [];
}

/** The catalogue's tools not yet enabled, in catalogue order. */
export function addableTools(
  catalogue: ProviderToolSpec[],
  entries: ProviderToolEntry[],
): ProviderToolSpec[] {
  const taken = new Set(entries.map((e) => e.type));
  return catalogue.filter((t) => !taken.has(t.type));
}
