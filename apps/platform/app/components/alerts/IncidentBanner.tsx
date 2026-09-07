import Link from "next/link";
import { Badge } from "./Badge";
import { SEVERITY_CLASS, describeEpisode, describeVerdict } from "./format";
import type { Incident } from "@/app/model/alerts";

/**
 * What is on fire right now.
 *
 * Above the watch list rather than inside it, because an open incident is a fact
 * about the installation and the list is a set of definitions — and because the
 * one thing somebody arriving at this page mid-outage needs is not to have to
 * find the row.
 *
 * Acknowledging records that somebody has seen it. It deliberately does not
 * resolve the episode: only the metric coming back does that, and a button that
 * closed an incident would be a button for lying to the next person who looks.
 */
export function IncidentBanner({
  incidents,
  busy,
  onAcknowledge,
}: {
  incidents: Incident[];
  busy: boolean;
  onAcknowledge: (id: string) => void;
}) {
  return (
    <section
      aria-label="Open incidents"
      className="mt-4 overflow-hidden rounded-lg border border-red-500/30"
    >
      <h2 className="bg-red-500/10 px-3 py-2 text-sm font-medium text-red-700 dark:text-red-300">
        {incidents.length} firing
      </h2>
      <ul className="divide-y divide-black/5 dark:divide-white/5">
        {incidents.map((incident) => (
          <li
            key={incident.id}
            className="flex items-center justify-between gap-3 px-3 py-2"
          >
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <Badge
                  label={incident.severity}
                  className={
                    SEVERITY_CLASS[incident.severity] ?? SEVERITY_CLASS.warning
                  }
                />
                <Link
                  href={`/platform/metrics/alerts/${encodeURIComponent(incident.watchId)}`}
                  className="truncate text-sm font-medium hover:underline"
                >
                  {incident.watchName}
                </Link>
              </div>
              <p className="mt-0.5 truncate text-xs text-zinc-500 dark:text-zinc-400">
                {describeVerdict(
                  {
                    matched: incident.openedMatched,
                    total: incident.openedTotal,
                  },
                  "when it opened",
                )}{" "}
                · {describeEpisode(incident)}
                {incident.notifications === 0 && " · nobody was told"}
              </p>
            </div>
            {incident.acknowledgedAt ? (
              <span className="shrink-0 text-xs text-zinc-500">
                Acknowledged
              </span>
            ) : (
              <button
                type="button"
                disabled={busy}
                onClick={() => onAcknowledge(incident.id)}
                className="shrink-0 rounded border border-black/10 px-2 py-1 text-xs hover:bg-black/5 disabled:opacity-50 dark:border-white/10 dark:hover:bg-white/5"
              >
                Acknowledge
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
