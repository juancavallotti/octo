"use client";

import { Field, INPUT } from "@/app/components/admin/fields";
import { INTERVALS, rescale, stepFor, withCurrent } from "./resolution";
import type { WatchInput } from "@/app/model/alerts";

/** How often the watch is asked. That is all a schedule is. */
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
 * already covered: checking more often narrows the buckets, so a bucket count
 * left alone would silently shorten every window.
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
