"use client";

import { Field, INPUT } from "@/app/components/admin/fields";
import { INTERVALS, rescale, stepFor, withCurrent } from "./resolution";
import type { WatchInput } from "@/app/model/alerts";

/**
 * How often the watch is asked. That is all a schedule is.
 *
 * It used to carry three number fields — a bucket width, an interval and a hold
 * — and the hold is not a schedule at all: it is about not being told the same
 * thing twice, which now lives with the rest of that. Mixing them meant the one
 * section nobody could explain was also the one nobody could skip.
 */
export function Schedule({
  watch,
  onChange,
}: {
  watch: WatchInput;
  onChange: (next: WatchInput) => void;
}) {
  return (
    <Field
      label="Check"
      hint={`Each check reads the window every condition asks for, ending about ninety seconds ago — long enough that a ${stepFor(watch.intervalSeconds)}-second bucket is complete before it is judged.`}
    >
      <select
        value={String(watch.intervalSeconds)}
        aria-label="Check"
        onChange={(e) => onChange(recheck(watch, Number(e.target.value)))}
        className={`${INPUT} w-full sm:w-72`}
      >
        {withCurrent(INTERVALS, watch.intervalSeconds).map((p) => (
          <option key={p.seconds} value={p.seconds}>
            {p.label}
          </option>
        ))}
      </select>
    </Field>
  );
}

/**
 * Change how often a watch is checked, keeping every window covering the span it
 * already covered.
 *
 * Checking more often narrows the buckets, and a bucket count left alone would
 * then mean half the time it used to — silently, since the form shows durations
 * and nothing on screen would appear to have moved.
 */
function recheck(watch: WatchInput, intervalSeconds: number): WatchInput {
  const step = stepFor(intervalSeconds);
  return {
    ...watch,
    intervalSeconds,
    stepSeconds: step,
    conditions: rescale(watch.conditions, watch.stepSeconds, step),
  };
}
