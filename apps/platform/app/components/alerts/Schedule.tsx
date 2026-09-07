"use client";

import { Field, INPUT } from "@/app/components/admin/fields";
import { INTERVALS, withCurrent } from "./catalogue";
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
      hint="Each check reads the window every condition asks for, ending a minute or so ago — long enough that a bucket is complete before it is judged."
    >
      <select
        value={String(watch.intervalSeconds)}
        aria-label="Check"
        onChange={(e) =>
          onChange({ ...watch, intervalSeconds: Number(e.target.value) })
        }
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
