import { Badge } from "./Badge";
import {
  STATUS_CLASS,
  describeOutcome,
  explainReason,
  formatValue,
} from "./format";
import type { WatchPreview } from "@/app/model/alerts";

/**
 * What this definition would have decided, right now.
 *
 * The point of showing every outcome rather than a verdict is the condition that
 * did *not* hold: tuning a spike is a matter of seeing that the observed value
 * was 3 against a baseline of 1 and being told that the change was too small to
 * care about — which is a sentence, not a boolean.
 */
export function PreviewPanel({ preview }: { preview: WatchPreview }) {
  return (
    <section
      aria-label="Preview result"
      className="mt-3 rounded-lg border border-black/10 p-3 dark:border-white/10"
    >
      <div className="flex flex-wrap items-center gap-2">
        <Badge
          label={preview.status}
          className={STATUS_CLASS[preview.status] ?? STATUS_CLASS.ok}
        />
        <span className="text-sm">
          {preview.matched} of {preview.total} matched
        </span>
        {preview.degraded && (
          <span
            className="text-xs text-amber-600 dark:text-amber-400"
            title="At least one condition could not be answered"
          >
            partly blind
          </span>
        )}
        {preview.windowTo && (
          <span className="text-xs text-zinc-500">
            window ending {new Date(preview.windowTo).toLocaleTimeString()}
          </span>
        )}
      </div>

      <ul className="mt-2 flex flex-col gap-1.5">
        {preview.outcomes.map((o) => (
          <li key={o.conditionId} className="flex items-start gap-2 text-xs">
            <span
              aria-hidden
              className={
                o.verdict === "true"
                  ? "text-emerald-600 dark:text-emerald-400"
                  : o.verdict === "false"
                    ? "text-zinc-400"
                    : "text-amber-600 dark:text-amber-400"
              }
            >
              {o.verdict === "true" ? "●" : o.verdict === "false" ? "○" : "◐"}
            </span>
            <span className="min-w-0">
              <span className="font-medium">{o.label}</span>
              <span className="text-zinc-500 dark:text-zinc-400">
                {" "}
                — {describeOutcome(o)}
              </span>
              {o.reason && o.reason !== "condition_met" && (
                <span className="block text-zinc-500 dark:text-zinc-400">
                  {explainReason(o.reason)}
                </span>
              )}
              {o.samples > 0 && (
                <span className="block text-zinc-400">
                  {o.samples} buckets
                  {o.baselineSamples
                    ? `, ${o.baselineSamples} of baseline`
                    : ""}
                  {o.denominator
                    ? `, over ${formatValue(o.denominator)} requests`
                    : ""}
                </span>
              )}
            </span>
          </li>
        ))}
      </ul>

      <p className="mt-2 text-xs text-zinc-500 dark:text-zinc-400">
        A preview records nothing and notifies nobody.
      </p>
    </section>
  );
}
