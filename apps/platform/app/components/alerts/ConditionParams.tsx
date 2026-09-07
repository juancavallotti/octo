"use client";

import { Field, INPUT } from "@/app/components/admin/fields";
import type { AlertCondition } from "@/app/model/alerts";

/**
 * How a condition is judged, in minutes.
 *
 * Minutes rather than "buckets" because the bucket width is fixed at a minute —
 * see catalogue.ts. "Window (buckets): 5" asked somebody to know a width that was
 * on a different part of the form; "over the last 5 minutes" does not.
 *
 * Only the parameters worth an opinion are here. A spike has a dozen knobs and
 * most have defaults chosen so that a quiet app behaves — putting them all on
 * screen would suggest they all want tuning, when the honest advice is to leave
 * them and press Try it now. The rest are still settable over the API.
 */
export function ConditionParams({
  condition,
  index,
  unit,
  onChange,
}: {
  condition: AlertCondition;
  index: number;
  unit?: string;
  onChange: (next: AlertCondition) => void;
}) {
  const params = condition.params ?? {};
  const set = (key: string, value: unknown) =>
    onChange({ ...condition, params: { ...params, [key]: value } });

  return (
    <div className="mt-3 grid gap-3 sm:grid-cols-3">
      {condition.type === "threshold" && (
        <>
          <Field label="When it is">
            <select
              value={String(params.op ?? "gt")}
              aria-label={`Condition ${index + 1} comparison`}
              onChange={(e) => set("op", e.target.value)}
              className={`${INPUT} w-full`}
            >
              <option value="gt">above</option>
              <option value="gte">at or above</option>
              <option value="lt">below</option>
              <option value="lte">at or below</option>
            </select>
          </Field>
          <Field
            label={unit === "ratio" ? "This rate" : "This number"}
            hint={unit === "ratio" ? "0.05 is 5%" : undefined}
          >
            <input
              value={String(params.threshold ?? "")}
              inputMode="decimal"
              aria-label={`Condition ${index + 1} threshold`}
              onChange={(e) => set("threshold", numeric(e.target.value))}
              className={`${INPUT} w-full font-mono`}
            />
          </Field>
          <Minutes
            label="Over the last"
            value={params.windowBuckets}
            index={index}
            name="window"
            onChange={(v) => set("windowBuckets", v)}
          />
        </>
      )}

      {condition.type === "spike" && (
        <>
          <Field label="In which direction">
            <select
              value={String(params.direction ?? "up")}
              aria-label={`Condition ${index + 1} direction`}
              onChange={(e) => set("direction", e.target.value)}
              className={`${INPUT} w-full`}
            >
              <option value="up">jumped up</option>
              <option value="down">dropped</option>
            </select>
          </Field>
          <Minutes
            label="Comparing the last"
            value={params.windowBuckets}
            index={index}
            name="window"
            onChange={(v) => set("windowBuckets", v)}
          />
          <Minutes
            label="Against the previous"
            value={params.baselineBuckets}
            index={index}
            name="baseline"
            hint="What counts as normal. It needs at least a dozen minutes that reported before it will fire at all."
            onChange={(v) => set("baselineBuckets", v)}
          />
        </>
      )}

      {condition.type === "absence" && (
        <Minutes
          label="Silent for"
          value={params.forBuckets}
          index={index}
          name="silence"
          hint="It also has to have been reporting beforehand, or its silence means nothing."
          onChange={(v) => set("forBuckets", v)}
        />
      )}
    </div>
  );
}

function Minutes({
  label,
  value,
  index,
  name,
  hint,
  onChange,
}: {
  label: string;
  value: unknown;
  index: number;
  name: string;
  hint?: string;
  onChange: (value: number | undefined) => void;
}) {
  return (
    <Field label={`${label} (minutes)`} hint={hint}>
      <input
        value={value === undefined || value === null ? "" : String(value)}
        inputMode="numeric"
        aria-label={`Condition ${index + 1} ${name}`}
        onChange={(e) => onChange(numeric(e.target.value))}
        className={`${INPUT} w-full font-mono`}
      />
    </Field>
  );
}

/**
 * Read a number out of a field, leaving it undefined while it is empty or
 * half-typed.
 *
 * Undefined rather than zero, because the service reads an absent parameter as
 * "use the default" and a zero as a value. Coercing "" to 0 mid-keystroke would
 * quietly set a threshold of zero on a field somebody was clearing.
 */
function numeric(raw: string): number | undefined {
  const trimmed = raw.trim();
  if (trimmed === "") return undefined;
  const value = Number(trimmed);
  return Number.isFinite(value) ? value : undefined;
}
