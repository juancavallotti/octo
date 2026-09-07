"use client";

import { useState } from "react";
import Link from "next/link";
import { BellRing, Plus } from "lucide-react";
import { EmptyState } from "@/app/(session)/platform/DashboardTiles";
import { acknowledgeIncident } from "@/app/model/alerts";
import { IncidentBanner } from "./IncidentBanner";
import { WatchTable } from "./WatchTable";
import { useAlerts } from "./useAlerts";

/**
 * The alerts tab: what is on fire, and every watch that could put it there.
 *
 * Open incidents lead, because "what is wrong right now" is the first question
 * anyone brings to this page, and a list of watches sorted by name does not
 * answer it. The watch list underneath is the one you edit.
 *
 * The reading lives in useAlerts; this is the shell around it.
 */
export default function AlertsMonitor() {
  const { watches, incidents, error, reload } = useAlerts();
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const acknowledge = async (id: string) => {
    setBusy(true);
    setActionError(null);
    try {
      await acknowledgeIncident(id);
      await reload();
    } catch (e) {
      setActionError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const shown = actionError ?? error;

  return (
    <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
      <div className="mx-auto w-full max-w-5xl">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h1 className="text-lg font-semibold">Alerts</h1>
            <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
              Standing questions about your telemetry. Each is asked on its own
              schedule, and tells somebody when the answer changes.
            </p>
          </div>
          <Link
            href="/platform/metrics/alerts/new"
            className="flex shrink-0 items-center gap-1.5 rounded-lg bg-zinc-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
          >
            <Plus size={14} />
            New watch
          </Link>
        </div>

        {shown && (
          <p
            role="alert"
            className="mt-4 rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400"
          >
            {shown}
          </p>
        )}

        {incidents.length > 0 && (
          <IncidentBanner
            incidents={incidents}
            busy={busy}
            onAcknowledge={acknowledge}
          />
        )}

        <div className="mt-6">
          {watches === null ? (
            <p className="text-sm text-zinc-500">Loading…</p>
          ) : watches.length === 0 ? (
            <EmptyState
              icon={BellRing}
              title="No watches yet"
              body="A watch asks something about your traces, logs or pod stats on a schedule — an error rate above a threshold, a sudden jump, an app that has gone quiet — and tells somebody when it holds."
            />
          ) : (
            <WatchTable items={watches} />
          )}
        </div>
      </div>
    </div>
  );
}
