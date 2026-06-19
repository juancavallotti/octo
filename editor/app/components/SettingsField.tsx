"use client";

import type { FieldSpec } from "@/app/schema/types";
import StringListEditor from "./fields/StringListEditor";
import StringMapEditor from "./fields/StringMapEditor";
import ReferenceField from "./fields/ReferenceField";

/** Shared input styling, matching the sidebar filter input. */
const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * Renders one block setting as a labelled, controlled input chosen by the
 * field's schema type. The parent (SettingsPanel) owns the value and persists
 * changes via `onChange`. Slot fields (flow/flow-list/case-list) are never passed
 * here — they're edited on the canvas. Collection editors (string-list,
 * string-map) arrive in a later change; for now they show a placeholder.
 */
export default function SettingsField({
  field,
  value,
  onChange,
}: {
  field: FieldSpec;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  return (
    <div className="flex flex-col gap-1">
      <label
        htmlFor={field.name}
        className="text-xs font-medium text-zinc-600 dark:text-zinc-300"
      >
        {field.label}
        {field.required && <span className="text-red-500"> *</span>}
      </label>
      {renderInput(field, value, onChange)}
      {field.description && (
        <p className="text-xs text-zinc-400 dark:text-zinc-500">
          {field.description}
        </p>
      )}
    </div>
  );
}

function renderInput(
  field: FieldSpec,
  value: unknown,
  onChange: (value: unknown) => void,
) {
  // A reference field (to a connection/flow) renders as a dropdown of valid
  // targets regardless of its underlying scalar type.
  if (field.ref) {
    return (
      <ReferenceField
        spec={field.ref}
        value={value}
        required={field.required}
        onChange={onChange}
      />
    );
  }

  switch (field.type) {
    case "boolean":
      return (
        <label className="flex items-center gap-2 text-sm text-zinc-600 dark:text-zinc-300">
          <input
            id={field.name}
            type="checkbox"
            checked={Boolean(value)}
            onChange={(e) => onChange(e.target.checked)}
            className="h-4 w-4 accent-sky-500"
          />
          <span className="text-xs text-zinc-400 dark:text-zinc-500">
            {value ? "Enabled" : "Disabled"}
          </span>
        </label>
      );

    case "number":
      return (
        <input
          id={field.name}
          type="number"
          value={value === undefined || value === null ? "" : String(value)}
          onChange={(e) =>
            onChange(e.target.value === "" ? undefined : Number(e.target.value))
          }
          className={INPUT}
        />
      );

    case "enum":
      return (
        <select
          id={field.name}
          value={value === undefined || value === null ? "" : String(value)}
          onChange={(e) =>
            onChange(e.target.value === "" ? undefined : e.target.value)
          }
          className={INPUT}
        >
          {!field.required && <option value="">—</option>}
          {(field.enum ?? []).map((opt) => (
            <option key={opt} value={opt}>
              {opt}
            </option>
          ))}
        </select>
      );

    case "cel":
      return (
        <textarea
          id={field.name}
          rows={2}
          value={value === undefined || value === null ? "" : String(value)}
          onChange={(e) => onChange(e.target.value)}
          placeholder="CEL expression"
          className={`${INPUT} resize-y font-mono`}
        />
      );

    case "string-list":
      return <StringListEditor value={value} onChange={onChange} />;

    case "string-map":
      return <StringMapEditor value={value} onChange={onChange} />;

    default:
      // string and any unknown scalar.
      return (
        <input
          id={field.name}
          type="text"
          value={value === undefined || value === null ? "" : String(value)}
          onChange={(e) => onChange(e.target.value)}
          className={INPUT}
        />
      );
  }
}
