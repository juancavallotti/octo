import Link from "next/link";
import { Badge } from "./Badge";
import {
  PHASE_CLASS,
  PHASE_LABEL,
  SEVERITY_CLASS,
  describeSchedule,
  formatValue,
} from "./format";
import type { WatchListItem } from "@/app/model/alerts";
import { relativeAge } from "@/app/lib/relativeAge";

/**
 * Every watch, with where its state machine has got to.
 *
 * The bordered-div-and-table idiom the pod stats and log tables already use.
 * What each row has to answer without being opened: is it firing, is it even
 * being evaluated, and when did it last run — that last one because a watch that
 * silently stopped being evaluated looks exactly like one that keeps finding
 * nothing, and the difference is the whole reason the execution log exists.
 */
export function WatchTable({ items }: { items: WatchListItem[] }) {
  return (
    <div className="overflow-x-auto rounded-lg border border-black/10 dark:border-white/10">
      <table className="w-full text-left text-xs">
        <thead className="bg-black/[0.02] text-zinc-500 dark:bg-white/[0.03] dark:text-zinc-400">
          <tr>
            <Th>Watch</Th>
            <Th>State</Th>
            <Th>Last value</Th>
            <Th>Schedule</Th>
            <Th>Last run</Th>
          </tr>
        </thead>
        <tbody className="divide-y divide-black/5 dark:divide-white/5">
          {items.map(({ watch, state }) => (
            <tr key={watch.id} className="align-top">
              <td className="px-3 py-2">
                <Link
                  href={`/platform/metrics/alerts/${encodeURIComponent(watch.id)}`}
                  className="font-medium hover:underline"
                >
                  {watch.name}
                </Link>
                <div className="mt-0.5 flex flex-wrap items-center gap-1.5">
                  <Badge
                    label={watch.severity}
                    className={
                      SEVERITY_CLASS[watch.severity] ?? SEVERITY_CLASS.warning
                    }
                  />
                  <span className="text-zinc-500 dark:text-zinc-400">
                    {watch.conditions.length}{" "}
                    {watch.conditions.length === 1 ? "condition" : "conditions"}{" "}
                    · {watch.combinator}
                  </span>
                </div>
              </td>
              <td className="px-3 py-2">
                <div className="flex flex-col items-start gap-1">
                  <Badge
                    label={PHASE_LABEL[state.phase] ?? state.phase}
                    className={PHASE_CLASS[state.phase] ?? PHASE_CLASS.ok}
                  />
                  {!watch.enabled && (
                    <span className="text-zinc-500">disabled</span>
                  )}
                  {muted(state.mutedUntil) && (
                    <span
                      className="text-zinc-500"
                      title="Still evaluated; notifications are suppressed"
                    >
                      muted
                    </span>
                  )}
                </div>
              </td>
              <td className="px-3 py-2 font-mono">
                {formatValue(state.lastValue)}
              </td>
              <td className="px-3 py-2 text-zinc-500 dark:text-zinc-400">
                {describeSchedule(watch)}
              </td>
              <td className="px-3 py-2 text-zinc-500 dark:text-zinc-400">
                {state.lastEvalAt ? (
                  relativeAge(state.lastEvalAt)
                ) : (
                  // Not "never": a watch created a moment ago has not run yet,
                  // and that is a different fact from one that has stopped.
                  <span title="It has not been evaluated yet">not yet</span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Th({ children }: { children: React.ReactNode }) {
  return <th className="px-3 py-2 font-medium">{children}</th>;
}

/**
 * A mute only counts while it lasts. The column is left set after one expires —
 * the service has no reason to clear it, and the state machine reads it against
 * the clock — so a truthiness check here would label a watch muted forever after
 * somebody silenced it once.
 */
function muted(until: string | null): boolean {
  return until !== null && Date.parse(until) > Date.now();
}
