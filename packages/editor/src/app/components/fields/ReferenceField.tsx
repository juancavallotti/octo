"use client";

import { referenceOptions } from "../../model/identity";
import type { ReferenceSpec } from "../../schema/types";
import { useEditorState } from "../../state/editorState";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * A dropdown for a setting that references another named entity in the document:
 * a connection (optionally narrowed to one connector type), a flow, or a declared
 * template resource. The options are the matching names currently in the document.
 * Connector references always allow an empty choice (falls back to the runtime's
 * default connector); flow and template references only when the field is optional.
 * A current value that no longer matches anything is still shown, flagged as
 * missing, so dangling references surface instead of silently vanishing.
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

  const options = referenceOptions(doc, spec);

  const current = value === undefined || value === null ? "" : String(value);
  const allowEmpty = spec.kind === "connector" || !required;
  const dangling = current !== "" && !options.includes(current);
  // Render an empty row when the field permits one, or whenever nothing is
  // selected yet — even for a required field. Without a placeholder for the empty
  // state the browser shows the first real option as selected while the model
  // value stays empty, so selecting that option never fires `onChange` and the
  // setting is never stored (the user believes they picked it, but they didn't).
  // The placeholder resolves to empty, which the pre-flight validation then flags
  // as required-and-missing.
  const showEmpty = allowEmpty || current === "";
  const emptyLabel =
    spec.kind === "connector" ? "— (default)" : allowEmpty ? "—" : "— select —";

  return (
    <select
      value={current}
      onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)}
      className={INPUT}
    >
      {showEmpty && <option value="">{emptyLabel}</option>}
      {dangling && <option value={current}>{current} (missing)</option>}
      {options.map((name) => (
        <option key={name} value={name}>
          {name}
        </option>
      ))}
    </select>
  );
}
