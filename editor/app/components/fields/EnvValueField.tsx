"use client";

import { useEditorState } from "@/app/state/editorState";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** Wrap a bare variable name as the `${NAME}` reference stored in settings. */
const wrap = (name: string) => "${" + name + "}";

/** Pull the variable name out of a `${NAME}` reference, or "" if it isn't one. */
function nameOf(value: unknown): string {
  const m = typeof value === "string" ? /^\$\{([^}]+)\}$/.exec(value) : null;
  return m ? m[1] : "";
}

/**
 * True when a value is a *whole-value* `${NAME}` env reference — used to seed env
 * mode. Embedded interpolation (e.g. `https://${HOST}/x`) is intentionally not a
 * match: that's ordinary text the picker can't represent, so it stays literal.
 */
export function isEnvRef(value: unknown): boolean {
  return nameOf(value) !== "";
}

/**
 * A dropdown for entering an environment variable into a typed setting (number,
 * boolean, enum) that otherwise can't hold the `${NAME}` syntax. Options are the
 * document's declared variables; the stored value is `${NAME}`, which the runtime
 * resolves to its native type at startup. Mirrors ReferenceField: a current value
 * that is no longer declared is still shown, flagged, so it doesn't silently
 * vanish.
 */
export default function EnvValueField({
  value,
  onChange,
}: {
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  const { state } = useEditorState();
  const names = state.document.env.map((v) => v.name).filter(Boolean);

  const selected = nameOf(value);
  const dangling = selected !== "" && !names.includes(selected);

  return (
    <select
      value={selected}
      onChange={(e) => onChange(e.target.value === "" ? undefined : wrap(e.target.value))}
      className={INPUT}
    >
      <option value="">
        {names.length === 0 ? "— no variables declared —" : "— select a variable —"}
      </option>
      {dangling && <option value={selected}>{selected} (not declared)</option>}
      {names.map((name) => (
        <option key={name} value={name}>
          {name}
        </option>
      ))}
    </select>
  );
}
