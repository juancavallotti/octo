"use client";

import { Field, INPUT } from "@/app/components/admin/fields";
import {
  BASELINES,
  WINDOWS,
  bucketsFor,
  secondsFor,
  withCurrent,
} from "./resolution";
import type { AlertCondition } from "@/app/model/alerts";

/**
 * How a condition is judged, in durations.
 *
 * Never in buckets. "Window (buckets): 5" asked somebody to know a width that
 * lived on a different part of the form; "over the last 5 minutes" does not, and
 * it stays true when the width changes because the count is recomputed rather
 * than the label.
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
  step,
  onChange,
}: {
  condition: AlertCondition;
  index: number;
  unit?: string;
  /** The bucket width this watch is measured in, which durations convert by. */
  step: number;
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
          <Span
            label="Over the last"
            options={WINDOWS}
            buckets={params.windowBuckets}
            step={step}
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
          <Span
            label="Comparing the last"
            options={WINDOWS}
            buckets={params.windowBuckets}
            step={step}
            index={index}
            name="window"
            onChange={(v) => set("windowBuckets", v)}
          />
          <Span
            label="Against the previous"
            options={BASELINES}
            buckets={params.baselineBuckets}
            step={step}
            index={index}
            name="baseline"
            hint="What counts as normal. It needs at least a dozen reporting buckets before it will fire at all."
            onChange={(v) => set("baselineBuckets", v)}
          />
        </>
      )}

      {condition.type === "absence" && (
        <Span
          label="Silent for"
          options={WINDOWS}
          buckets={params.forBuckets}
          step={step}
          index={index}
          name="silence"
          hint="It also has to have been reporting beforehand, or its silence means nothing."
          onChange={(v) => set("forBuckets", v)}
        />
      )}
    </div>
  );
}

/**
 * A span, chosen as a duration and stored as a count of buckets.
 *
 * The conversion is here rather than at save time so the select always reflects
 * what is stored: a watch whose width changed under it shows the span it now
 * covers, not the one somebody originally picked.
 */
function Span({
  label,
  options,
  buckets,
  step,
  index,
  name,
  hint,
  onChange,
}: {
  label: string;
  options: { seconds: number; label: string }[];
  buckets: unknown;
  step: number;
  index: number;
  name: string;
  hint?: string;
  onChange: (buckets: number) => void;
}) {
  const current =
    typeof buckets === "number"
      ? secondsFor(buckets, step)
      : options[0].seconds;
  return (
    <Field label={label} hint={hint}>
      <select
        value={String(current)}
        aria-label={`Condition ${index + 1} ${name}`}
        onChange={(e) => onChange(bucketsFor(Number(e.target.value), step))}
        className={`${INPUT} w-full`}
      >
        {withCurrent(options, current).map((option) => (
          <option key={option.seconds} value={option.seconds}>
            {option.label}
          </option>
        ))}
      </select>
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
