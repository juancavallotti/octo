"use client";

import { Field, INPUT } from "@/app/components/admin/fields";
import { HOLDS, REPEATS, withCurrent } from "./catalogue";
import type { AlertNoData, WatchInput } from "@/app/model/alerts";

/**
 * Not being told the same thing twice.
 *
 * Three separate questions, and none of them is "how often do we look":
 *
 *   the hold      how long it has to keep being true before you hear about it
 *   the repeat    whether you hear again while it is still true
 *   no data       what an empty window counts as
 *
 * The hold used to sit under Schedule as "Hold for (seconds)", which explained
 * nothing. It is here because what it is for is suppression: a momentary spike
 * that resolves on its own is not something to be woken for.
 */
export function Deduplication({
  watch,
  onChange,
}: {
  watch: WatchInput;
  onChange: (next: WatchInput) => void;
}) {
  const checks = Math.max(
    1,
    Math.ceil(watch.forSeconds / Math.max(1, watch.intervalSeconds)),
  );

  return (
    <div className="flex flex-col gap-3">
      <Field
        label="Wait before telling me"
        hint={
          watch.forSeconds > 0
            ? `It has to be true on ${checks} checks in a row. A check that could not decide breaks the run rather than counting toward it.`
            : "The first check that finds it true raises the alert."
        }
      >
        <select
          value={String(watch.forSeconds)}
          aria-label="Wait before telling me"
          onChange={(e) =>
            onChange({ ...watch, forSeconds: Number(e.target.value) })
          }
          className={`${INPUT} w-full sm:w-80`}
        >
          {withCurrent(HOLDS, watch.forSeconds).map((p) => (
            <option key={p.seconds} value={p.seconds}>
              {p.label}
            </option>
          ))}
        </select>
      </Field>

      <Field
        label="Tell me again"
        hint="Resolving is announced either way, and only once."
      >
        <select
          value={String(watch.renotifySeconds)}
          aria-label="Tell me again"
          onChange={(e) =>
            onChange({ ...watch, renotifySeconds: Number(e.target.value) })
          }
          className={`${INPUT} w-full sm:w-80`}
        >
          {withCurrent(REPEATS, watch.renotifySeconds).map((p) => (
            <option key={p.seconds} value={p.seconds}>
              {p.label}
            </option>
          ))}
        </select>
      </Field>

      <Field
        label="If there is nothing to measure"
        hint="A quiet app is the ordinary reason a window is empty, so the default does not treat it as a problem. To alert on silence, add a “Stopped reporting” condition instead — it also checks the app was reporting beforehand."
      >
        <select
          value={watch.onNoData}
          aria-label="If there is nothing to measure"
          onChange={(e) =>
            onChange({ ...watch, onNoData: e.target.value as AlertNoData })
          }
          className={`${INPUT} w-full sm:w-80`}
        >
          <option value="ok">Treat it as fine</option>
          <option value="fire">Treat it as a problem</option>
          <option value="keep">Leave the watch where it is</option>
        </select>
      </Field>
    </div>
  );
}
