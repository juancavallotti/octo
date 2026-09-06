"use client";

import { Field, INPUT } from "./fields";

/**
 * The three retention windows as a form.
 *
 * Split out of RetentionSettingsManager because the manager owns loading,
 * saving, sweeping and confirming, and the fields are the one part of it that is
 * only markup. Three near-identical blocks inline made the file longer than the
 * lint cap and, more to the point, buried the interesting logic under them.
 */

/** A window as the form holds it: a string, so a field can be empty mid-edit. */
export type Draft = { logs: string; traces: string; alerts: string };

/** Which field an edit came from, so the manager does not repeat the shape. */
export type Window = keyof Draft;

export function RetentionWindows({
  draft,
  parsed,
  busy,
  describe,
  onChange,
}: {
  draft: Draft;
  /** The parsed value of each field, or null where it is not storable. */
  parsed: Record<Window, number | null>;
  busy: boolean;
  describe: (days: number) => string;
  onChange: (window: Window, value: string) => void;
}) {
  return (
    <>
      <RetentionWindow
        window="logs"
        label="Keep logs for (days)"
        value={draft.logs}
        days={parsed.logs}
        busy={busy}
        describe={describe}
        onChange={onChange}
      />
      <RetentionWindow
        window="traces"
        label="Keep traces for (days)"
        value={draft.traces}
        days={parsed.traces}
        busy={busy}
        describe={describe}
        onChange={onChange}
      />
      <RetentionWindow
        window="alerts"
        label="Keep alert history for (days)"
        value={draft.alerts}
        days={parsed.alerts}
        busy={busy}
        // Zero reads differently here than on the other two axes, and the hint
        // is where that gets said. This is the one stream that grows whether or
        // not anything happens — a watch records an evaluation every time it
        // runs, including the ones that found nothing — so "forever" is a
        // choice worth spelling out rather than a quiet default.
        describe={(days) =>
          days === 0
            ? "Kept forever — including every evaluation that found nothing."
            : describe(days)
        }
        onChange={onChange}
      />
    </>
  );
}

function RetentionWindow({
  window,
  label,
  value,
  days,
  busy,
  describe,
  onChange,
}: {
  window: Window;
  label: string;
  value: string;
  days: number | null;
  busy: boolean;
  describe: (days: number) => string;
  onChange: (window: Window, value: string) => void;
}) {
  return (
    <Field label={label} hint={days === null ? undefined : describe(days)}>
      <input
        value={value}
        disabled={busy}
        inputMode="numeric"
        aria-label={label}
        onChange={(e) => onChange(window, e.target.value)}
        className={`${INPUT} w-full font-mono`}
      />
    </Field>
  );
}
