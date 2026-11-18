"use client";

import { X } from "lucide-react";
import { useProjectView } from "../../state/useProject";
import SettingsField from "../SettingsField";
import {
  addableTools,
  catalogueFor,
  serializeEntries,
  toEntries,
  type ProviderToolEntry,
} from "./providerTools";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

const HINT = "text-xs text-zinc-400 dark:text-zinc-500";

/**
 * Editor for an AI block's `providerTools` (the `provider-tool-list` field type):
 * the server-side tools the block's LLM connection runs, such as web search. The
 * tools on offer and their fields are the selected connection's catalogue, so the
 * list follows the `connection` it is handed. A stored tool the catalogue does not
 * list is kept and flagged rather than dropped, so switching connections never
 * silently discards configuration. It is fully controlled: `value` is the only
 * state, so an outside change to it, such as an undo, is what renders next.
 */
export default function ProviderToolListEditor({
  value,
  connection,
  onChange,
}: {
  value: unknown;
  connection: unknown;
  onChange: (value: ProviderToolEntry[] | undefined) => void;
}) {
  const catalogue = catalogueFor(useProjectView(), connection);
  const entries = toEntries(value);

  function commit(next: ProviderToolEntry[]) {
    onChange(next.length > 0 ? serializeEntries(next) : undefined);
  }

  function setSetting(i: number, name: string, v: unknown) {
    commit(
      entries.map((e, j) =>
        j === i ? { ...e, settings: { ...e.settings, [name]: v } } : e,
      ),
    );
  }

  if (catalogue === undefined && entries.length === 0) {
    return <p className={HINT}>Select an LLM connection to see its provider tools.</p>;
  }
  const offered = catalogue ? addableTools(catalogue, entries) : [];

  return (
    <div className="flex flex-col gap-2">
      {entries.map((entry, i) => {
        const tool = catalogue?.find((t) => t.type === entry.type);
        return (
          <div
            key={entry.type || i}
            className="flex flex-col gap-2 rounded-md border border-black/10 dark:border-white/15 p-2"
          >
            <div className="flex items-center gap-1.5">
              <span className="text-sm font-medium">{tool?.label ?? entry.type}</span>
              {!tool && catalogue !== undefined && (
                <span className="text-xs text-amber-600">not offered by this connection</span>
              )}
              <button
                type="button"
                aria-label={`Remove ${tool?.label ?? entry.type}`}
                onClick={() => commit(entries.filter((_, j) => j !== i))}
                className="ml-auto shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
              >
                <X size={14} />
              </button>
            </div>
            {tool?.description && <p className={HINT}>{tool.description}</p>}
            {(tool?.fields ?? []).map((field) => (
              <SettingsField
                key={field.name}
                field={field}
                value={entry.settings?.[field.name]}
                onChange={(v) => setSetting(i, field.name, v)}
              />
            ))}
          </div>
        );
      })}
      {catalogue !== undefined && catalogue.length === 0 && (
        <p className={HINT}>This connection runs no provider tools.</p>
      )}
      {offered.length > 0 && (
        <select
          aria-label="Add provider tool"
          value=""
          onChange={(e) => {
            if (e.target.value) commit([...entries, { type: e.target.value }]);
          }}
          className={INPUT}
        >
          <option value="">+ Add provider tool</option>
          {offered.map((t) => (
            <option key={t.type} value={t.type}>
              {t.label}
            </option>
          ))}
        </select>
      )}
    </div>
  );
}
