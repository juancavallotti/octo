"use client";

import { flowNames } from "@/app/model/identity";
import type { ReferenceSpec } from "@/app/schema/types";
import { useEditorState } from "@/app/state/editorState";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * A dropdown for a setting that references another named entity in the document:
 * a connection (optionally narrowed to one connector type) or a flow. The options
 * are the matching names currently in the document. Connector references always
 * allow an empty choice (falls back to the runtime's default connector); flow
 * references only when the field is optional. A current value that no longer
 * matches anything is still shown, flagged as missing, so dangling references
 * surface instead of silently vanishing.
 */
export default function ReferenceField({
  spec,
  value,
  required,
  onChange,
}: {
  spec: ReferenceSpec;
  value: unknown;
  required: boolean;
  onChange: (value: unknown) => void;
}) {
  const { state } = useEditorState();
  const doc = state.document;

  const options =
    spec.kind === "connector"
      ? doc.connectors
          .filter((c) => c.type === spec.connectorType && c.name)
          .map((c) => c.name)
      : Array.from(new Set(flowNames(doc)));

  const current = value === undefined || value === null ? "" : String(value);
  const allowEmpty = spec.kind === "connector" || !required;
  const dangling = current !== "" && !options.includes(current);

  return (
    <select
      value={current}
      onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)}
      className={INPUT}
    >
      {allowEmpty && (
        <option value="">
          {spec.kind === "connector" ? "— (default)" : "—"}
        </option>
      )}
      {dangling && <option value={current}>{current} (missing)</option>}
      {options.map((name) => (
        <option key={name} value={name}>
          {name}
        </option>
      ))}
    </select>
  );
}
