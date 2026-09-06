"use client";

import type { StatsPod } from "@/app/model/stats";
import { relativeAge } from "@/app/lib/relativeAge";
import { formatStep, parseGoDuration } from "@/app/components/stats/chart/duration";
import { shortPod } from "@/app/components/stats/chart/format";

/**
 * What each pod has stored, and whether it is still storing it.
 *
 * The two row counts are shown side by side rather than summed because their
 * difference is usually the answer to the reader's real question. Zero live rows
 * beside a full history is the ordinary state of a pod that stopped a few hours
 * ago — the live tier is kept for twice the rollup interval while the pod stays
 * indexed for the whole retention window — and a single total would make that
 * look like data loss.
 */
export default function PodStatsTable({
  pods,
  now,
}: {
  pods: StatsPod[];
  now: number;
}) {
  if (pods.length === 0) return null;

  return (
    <div className="overflow-x-auto rounded-xl border border-black/10 bg-white/40 dark:border-white/10 dark:bg-zinc-900/30">
      <table className="w-full text-left text-xs">
        <thead className="text-zinc-500">
          <tr>
            <Th>Pod</Th>
            <Th>Last seen</Th>
            <Th>Sample</Th>
            <Th>Bucket</Th>
            <Th>Series</Th>
            <Th>Rows (live / history)</Th>
          </tr>
        </thead>
        <tbody>
          {pods.map((pod) => (
            <tr key={pod.pod} className="border-t border-black/5 dark:border-white/5">
              <td className="px-3 py-2 font-mono" title={pod.pod}>
                <span className="flex items-center gap-2">
                  <span
                    aria-hidden
                    className={`inline-block h-1.5 w-1.5 rounded-full ${
                      pod.reporting ? "bg-emerald-500" : "bg-zinc-400"
                    }`}
                  />
                  {shortPod(pod.pod)}
                </span>
              </td>
              <td className="px-3 py-2 tabular-nums text-zinc-500">
                {relativeAge(pod.lastSeen, now) ?? "—"}
                {!pod.reporting && <span className="ml-1.5 text-zinc-400">(stopped)</span>}
              </td>
              <td className="px-3 py-2 tabular-nums text-zinc-500">{step(pod.sampleInterval)}</td>
              <td className="px-3 py-2 tabular-nums text-zinc-500">{step(pod.rollupInterval)}</td>
              <td className="px-3 py-2 tabular-nums text-zinc-500">{pod.series || "—"}</td>
              <td className="px-3 py-2 tabular-nums text-zinc-500">
                {pod.liveRows.toLocaleString()} / {pod.rollupRows.toLocaleString()}
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

/** A configured interval, said as shortly as it can be. */
function step(value: string): string {
  const ms = parseGoDuration(value);
  return ms === null ? value || "—" : formatStep(ms);
}
