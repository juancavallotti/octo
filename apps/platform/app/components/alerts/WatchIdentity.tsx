"use client";

import { Field, INPUT } from "@/app/components/admin/fields";
import type { AlertSeverity, WatchInput } from "@/app/model/alerts";

/**
 * What the watch is called, and how loudly it speaks.
 *
 * Name over description rather than beside it: a description is a sentence and a
 * name is a few words, and two boxes of equal width side by side said the
 * opposite. The description is a textarea for the same reason.
 */
export function WatchIdentity({
  watch,
  onChange,
}: {
  watch: WatchInput;
  onChange: (next: WatchInput) => void;
}) {
  return (
    <div className="flex flex-col gap-3">
      <Field label="Name">
        <input
          value={watch.name}
          aria-label="Name"
          onChange={(e) => onChange({ ...watch, name: e.target.value })}
          className={`${INPUT} w-full`}
          placeholder="Checkout error rate"
        />
      </Field>

      <Field
        label="Description"
        hint="What somebody reading the alert at three in the morning needs to know."
      >
        <textarea
          value={watch.description}
          aria-label="Description"
          rows={2}
          onChange={(e) => onChange({ ...watch, description: e.target.value })}
          className={`${INPUT} w-full`}
        />
      </Field>

      <div className="flex flex-wrap items-end gap-4">
        <Field label="Severity">
          <select
            value={watch.severity}
            aria-label="Severity"
            onChange={(e) =>
              onChange({ ...watch, severity: e.target.value as AlertSeverity })
            }
            className={`${INPUT} w-40`}
          >
            <option value="info">info</option>
            <option value="warning">warning</option>
            <option value="critical">critical</option>
          </select>
        </Field>
        <label className="flex items-center gap-2 pb-1 text-sm">
          <input
            type="checkbox"
            checked={watch.enabled}
            onChange={(e) => onChange({ ...watch, enabled: e.target.checked })}
          />
          Enabled
        </label>
      </div>
    </div>
  );
}
