"use client";

import { Search, Sparkles, Timer } from "lucide-react";
import type { TraceStatus } from "@/app/model/traces";
import type { TraceFilterValues } from "./query";

/**
 * The filter bar over one app's traces.
 *
 * Everything here narrows a list that only exists once an app has been chosen,
 * which is why it is all disabled until one has been. The window is the
 * exception and so it is not here: it measures the app list too, and one window
 * for both is what keeps an app row's "12 traces" and the list under it counting
 * the same thing. It sits with the app picker, above this, in the order the two
 * are decided.
 */

const CONTROL =
  "rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30";

const STATUSES: { key: TraceStatus; label: string; tone: string }[] = [
  { key: "ok", label: "OK", tone: "text-emerald-600 dark:text-emerald-400" },
  { key: "failed", label: "Failed", tone: "text-red-500" },
  { key: "dropped", label: "Dropped", tone: "text-amber-600 dark:text-amber-400" },
];

/** The "slower than" thresholds worth one click. */
const THRESHOLDS = [
  { ms: 0, label: "Any duration" },
  { ms: 100, label: "≥ 100ms" },
  { ms: 500, label: "≥ 500ms" },
  { ms: 1_000, label: "≥ 1s" },
  { ms: 5_000, label: "≥ 5s" },
];

export default function TraceFilters({
  value,
  disabled,
  onChange,
}: {
  value: TraceFilterValues;
  /** Whether the controls are inert, which they are until an app is chosen. */
  disabled?: boolean;
  onChange: (next: TraceFilterValues) => void;
}) {
  const set = (patch: Partial<TraceFilterValues>) => onChange({ ...value, ...patch });

  const toggleStatus = (status: TraceStatus) =>
    set({
      statuses: value.statuses.includes(status)
        ? value.statuses.filter((s) => s !== status)
        : [...value.statuses, status],
    });

  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-black/10 px-3 py-2 dark:border-white/10">
      <div className="flex items-center gap-1">
        {STATUSES.map((status) => {
          const on = value.statuses.includes(status.key);
          return (
            <button
              key={status.key}
              type="button"
              aria-pressed={on}
              disabled={disabled}
              onClick={() => toggleStatus(status.key)}
              className={`rounded-md border px-2 py-1 text-xs font-medium transition-colors disabled:opacity-50 ${
                on
                  ? `border-transparent bg-black/[0.07] ${status.tone} dark:bg-white/[0.10]`
                  : "border-black/10 text-zinc-500 hover:bg-black/[0.04] dark:border-white/15 dark:text-zinc-400 dark:hover:bg-white/[0.06]"
              }`}
            >
              {status.label}
            </button>
          );
        })}
      </div>

      <label className="flex items-center gap-1.5">
        <Timer size={14} className="text-zinc-400" />
        <select
          aria-label="Slower than"
          value={value.minDurationMs ?? 0}
          disabled={disabled}
          onChange={(e) => set({ minDurationMs: Number(e.target.value) || null })}
          className={CONTROL}
        >
          {THRESHOLDS.map((threshold) => (
            <option key={threshold.ms} value={threshold.ms}>
              {threshold.label}
            </option>
          ))}
        </select>
      </label>

      <button
        type="button"
        aria-pressed={value.hasLlm}
        disabled={disabled}
        onClick={() => set({ hasLlm: !value.hasLlm })}
        className={`inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-medium transition-colors disabled:opacity-50 ${
          value.hasLlm
            ? "border-transparent bg-violet-500/15 text-violet-600 dark:text-violet-300"
            : "border-black/10 text-zinc-500 hover:bg-black/[0.04] dark:border-white/15 dark:text-zinc-400 dark:hover:bg-white/[0.06]"
        }`}
      >
        <Sparkles size={13} />
        Model calls
      </button>

      <label className="ml-auto flex min-w-0 flex-1 items-center gap-1.5 sm:max-w-xs">
        <Search size={14} className="shrink-0 text-zinc-400" />
        <input
          type="search"
          aria-label="Search traces"
          placeholder="trace id, route or flow"
          value={value.q}
          disabled={disabled}
          onChange={(e) => set({ q: e.target.value })}
          className={`${CONTROL} w-full`}
        />
      </label>
    </div>
  );
}
