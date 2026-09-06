"use client";

import { Field, INPUT } from "@/app/components/admin/fields";
import { formatSeconds } from "./format";
import type {
  AlertNoData,
  AlertSeverity,
  WatchInput,
} from "@/app/model/alerts";

/**
 * One schedule for the whole watch.
 *
 * Nothing here is per condition, and that is the design rather than a
 * simplification: the watch is the unit somebody reasons about and the unit an
 * action fires for. Per-condition holds would let two clauses each be satisfied
 * for five minutes in non-overlapping stretches, with the conjunction never once
 * true — and fire.
 */
export function ScheduleFields({
  watch,
  onChange,
}: {
  watch: WatchInput;
  onChange: (next: WatchInput) => void;
}) {
  const set = <K extends keyof WatchInput>(key: K, value: WatchInput[K]) =>
    onChange({ ...watch, [key]: value });

  return (
    <div className="grid gap-3 sm:grid-cols-3">
      <Field
        label="Bucket width (seconds)"
        hint="The resolution every condition is measured in. One width for the watch, so conditions over the same rows share one query."
      >
        <Seconds
          value={watch.stepSeconds}
          label="Bucket width"
          onChange={(v) => set("stepSeconds", v)}
        />
      </Field>

      <Field
        label="Evaluate every (seconds)"
        hint={`= ${formatSeconds(watch.intervalSeconds)}`}
      >
        <Seconds
          value={watch.intervalSeconds}
          label="Interval"
          onChange={(v) => set("intervalSeconds", v)}
        />
      </Field>

      <Field
        label="Hold for (seconds)"
        hint={
          watch.forSeconds > 0
            ? `${Math.max(1, Math.ceil(watch.forSeconds / Math.max(1, watch.intervalSeconds)))} consecutive evaluations`
            : "Fires on the first evaluation that holds."
        }
      >
        <Seconds
          value={watch.forSeconds}
          label="Hold"
          onChange={(v) => set("forSeconds", v)}
        />
      </Field>

      <Field
        label="Repeat every (seconds)"
        hint={
          watch.renotifySeconds > 0
            ? `Says so again every ${formatSeconds(watch.renotifySeconds)} while it is firing.`
            : "Announces an episode once."
        }
      >
        <Seconds
          value={watch.renotifySeconds}
          label="Repeat"
          onChange={(v) => set("renotifySeconds", v)}
        />
      </Field>

      <Field label="Severity">
        <select
          value={watch.severity}
          aria-label="Severity"
          onChange={(e) => set("severity", e.target.value as AlertSeverity)}
          className={`${INPUT} w-full`}
        >
          <option value="info">info</option>
          <option value="warning">warning</option>
          <option value="critical">critical</option>
        </select>
      </Field>

      <Field
        label="When there is no data"
        hint="A quiet app is the ordinary reason a window is empty, so the default treats it as 'does not hold'."
      >
        <select
          value={watch.onNoData}
          aria-label="When there is no data"
          onChange={(e) => set("onNoData", e.target.value as AlertNoData)}
          className={`${INPUT} w-full`}
        >
          <option value="ok">does not hold</option>
          <option value="fire">holds</option>
          <option value="keep">leave the watch where it is</option>
        </select>
      </Field>
    </div>
  );
}

function Seconds({
  value,
  label,
  onChange,
}: {
  value: number;
  label: string;
  onChange: (value: number) => void;
}) {
  return (
    <input
      value={String(value)}
      inputMode="numeric"
      aria-label={label}
      onChange={(e) => {
        const next = Number(e.target.value.trim());
        onChange(Number.isFinite(next) ? next : 0);
      }}
      className={`${INPUT} w-full font-mono`}
    />
  );
}
