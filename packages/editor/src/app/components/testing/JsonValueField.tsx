"use client";

import { useState, type ReactNode } from "react";
import JsonEditor from "../../cel/JsonEditor";

const CODE =
  "rounded-md border border-black/10 dark:border-white/15 overflow-auto octo-code";

/**
 * A JSON value in the suite model, edited as text.
 *
 * The model holds VALUES where a form has to hold TEXT, and half-typed JSON is not a
 * value: a field that parsed on every keystroke and stored the result would erase what
 * you were in the middle of writing the moment you typed an opening brace. So the draft
 * text lives here, and only a value that parses is lifted. While it does not, the model
 * keeps whatever it last had and the field says why nothing is being saved.
 *
 * The draft is seeded once and deliberately never re-seeded from `value`. Re-seeding
 * would fight the typist: the parent stores a value, so what comes back has been through
 * JSON.stringify and is reformatted. Switching to another case remounts the field
 * instead — the detail pane is keyed on the case — which is the only moment the draft
 * should change out from under it.
 *
 * Empty text means the field is ABSENT, which is not the same as `null`: `body: null`
 * asserts the flow returned a null body, while no `body` asserts nothing about it.
 */
export default function JsonValueField({
  label,
  hint,
  value,
  onChange,
  placeholder,
  objectOnly,
  minHeight,
}: {
  label: string;
  /** Shown beside the label — the place to say what the field actually asserts. */
  hint?: ReactNode;
  /** The current value, or undefined when the field is absent. */
  value: unknown;
  /** Called with the parsed value, or undefined when the field is cleared. */
  onChange: (next: unknown) => void;
  placeholder?: string;
  /**
   * Reject anything but a JSON object. `vars` is a map in the format, so an array or a
   * string here is a file dolphin refuses — caught in the field rather than as a load
   * error naming the whole suite.
   */
  objectOnly?: boolean;
  minHeight?: number;
}) {
  const [text, setText] = useState(() =>
    value === undefined ? "" : JSON.stringify(value, null, 2),
  );
  const [problem, setProblem] = useState<string | null>(null);

  const change = (next: string) => {
    setText(next);
    if (next.trim() === "") {
      setProblem(null);
      onChange(undefined);
      return;
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(next);
    } catch {
      setProblem("That isn't valid JSON yet, so it isn't being saved.");
      return;
    }
    if (objectOnly && (parsed === null || typeof parsed !== "object" || Array.isArray(parsed))) {
      setProblem("This has to be a JSON object, e.g. { \"status\": \"200\" }.");
      return;
    }
    setProblem(null);
    onChange(parsed);
  };

  return (
    <label className="flex flex-col gap-1">
      <span className="flex items-baseline justify-between gap-2 text-xs font-medium text-zinc-500">
        {label}
        {hint && (
          <span className="text-right font-normal text-zinc-400 dark:text-zinc-500">
            {hint}
          </span>
        )}
      </span>
      <div className={`${CODE} ${problem ? "border-rose-400/70" : ""}`}>
        <JsonEditor
          value={text}
          onChange={change}
          placeholder={placeholder}
          minHeight={minHeight}
        />
      </div>
      {problem && <span className="text-[11px] text-rose-500">{problem}</span>}
    </label>
  );
}
