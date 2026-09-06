"use client";

import { Field, INPUT } from "@/app/components/admin/fields";
import { ScopeFields } from "./ScopeFields";
import type { AlertCondition } from "@/app/model/alerts";

/**
 * The parameters one condition kind takes, plus the scope it reads over.
 *
 * Only the parameters worth setting by hand are here. A spike has a dozen knobs
 * and eleven of them have defaults chosen to make a quiet series behave; putting
 * every one on the form would suggest they all want an opinion, when the honest
 * advice is to leave them alone and use Preview. The whole set is still settable
 * over the API for the case where somebody genuinely needs one.
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
          <Field label="Comparison">
            <select
              value={String(params.op ?? "gt")}
              aria-label={`Condition ${index + 1} comparison`}
              onChange={(e) => set("op", e.target.value)}
              className={`${INPUT} w-full`}
            >
              <option value="gt">is above</option>
              <option value="gte">is at or above</option>
              <option value="lt">is below</option>
              <option value="lte">is at or below</option>
            </select>
          </Field>
          <Field
            label="Threshold"
            hint={unit === "ratio" ? "A proportion: 0.05 is 5%" : undefined}
          >
            <input
              value={String(params.threshold ?? "")}
              inputMode="decimal"
              aria-label={`Condition ${index + 1} threshold`}
              onChange={(e) => set("threshold", numeric(e.target.value))}
              className={`${INPUT} w-full font-mono`}
            />
          </Field>
          <Buckets
            label="Window (buckets)"
            value={params.windowBuckets}
            index={index}
            name="window"
            onChange={(v) => set("windowBuckets", v)}
          />
        </>
      )}

      {condition.type === "spike" && (
        <>
          <Field label="Direction">
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
          <Buckets
            label="Window (buckets)"
            value={params.windowBuckets}
            index={index}
            name="window"
            onChange={(v) => set("windowBuckets", v)}
          />
          <Buckets
            label="Baseline (buckets)"
            value={params.baselineBuckets}
            index={index}
            name="baseline"
            hint="How much history counts as normal. It needs at least a dozen reporting buckets before it will fire at all."
            onChange={(v) => set("baselineBuckets", v)}
          />
        </>
      )}

      {condition.type === "absence" && (
        <Buckets
          label="Silent for (buckets)"
          value={params.forBuckets}
          index={index}
          name="silence"
          hint="It also has to have been reporting beforehand, or its silence means nothing."
          onChange={(v) => set("forBuckets", v)}
        />
      )}

      <ScopeFields condition={condition} index={index} onChange={onChange} />
    </div>
  );
}

function Buckets({
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
    <Field label={label} hint={hint}>
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
 * Undefined rather than zero, because the service treats an absent parameter as
 * "use the default" and a zero as a value. Coercing "" to 0 mid-keystroke would
 * quietly set a threshold of zero on a field somebody was in the middle of
 * clearing.
 */
function numeric(raw: string): number | undefined {
  const trimmed = raw.trim();
  if (trimmed === "") return undefined;
  const value = Number(trimmed);
  return Number.isFinite(value) ? value : undefined;
}
