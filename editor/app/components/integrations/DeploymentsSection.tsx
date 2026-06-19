"use client";

import { useCallback, useEffect, useState } from "react";
import { Rocket, Trash2 } from "lucide-react";
import {
  createDeployment,
  deleteDeployment,
  listDeployments,
  type Deployment,
  type DeploymentStatus,
} from "@/app/model/orchestrator";

/**
 * Deployments for one integration: a one-click Deploy plus a list of live
 * deployments with their status and an Undeploy action. The orchestrator
 * refreshes each deployment's status from the cluster on read, so the list is
 * re-fetched on mount and on a light interval while shown — no client-side
 * status tracking. Mutations refresh immediately, mirroring IntegrationsManager.
 */

// Status is refreshed server-side on read; poll gently so pending->running shows.
const REFRESH_MS = 4000;

const STATUS_STYLES: Record<DeploymentStatus, string> = {
  running: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  pending: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  failed: "bg-red-500/15 text-red-600 dark:text-red-400",
};

function StatusBadge({ status }: { status: DeploymentStatus }) {
  const cls = STATUS_STYLES[status] ?? "bg-zinc-500/15 text-zinc-500";
  return (
    <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${cls}`}>
      {status}
    </span>
  );
}

export default function DeploymentsSection({
  integrationId,
}: {
  integrationId: string;
}) {
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // A then-chain (not an async body) so the effect's call doesn't setState
  // synchronously — same shape as IntegrationsManager's refresh.
  const refresh = useCallback(
    () =>
      listDeployments(integrationId).then(
        (items) => {
          setDeployments(items);
          setError(null);
        },
        (e) => setError((e as Error).message),
      ),
    [integrationId],
  );

  useEffect(() => {
    refresh();
    const timer = setInterval(refresh, REFRESH_MS);
    return () => clearInterval(timer);
  }, [refresh]);

  /** Run a mutation, then refresh; surface failures inline. */
  const run = useCallback(
    async (fn: () => Promise<unknown>) => {
      setBusy(true);
      setError(null);
      try {
        await fn();
        await refresh();
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [refresh],
  );

  const deploy = () => run(() => createDeployment(integrationId));

  const undeploy = (d: Deployment) => {
    if (!confirm(`Undeploy "${d.name}" (${d.id.slice(0, 8)})?`)) return;
    run(() => deleteDeployment(d.id));
  };

  return (
    <>
      <div className="mb-2 flex justify-end">
        <button
          type="button"
          onClick={deploy}
          disabled={busy}
          className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500 disabled:opacity-50"
        >
          <Rocket size={14} />
          Deploy
        </button>
      </div>

      {error && <p className="mb-2 text-sm text-red-500">{error}</p>}

      {deployments.length === 0 ? (
        <p className="text-sm text-zinc-400">Not deployed.</p>
      ) : (
        <ul className="space-y-1">
          {deployments.map((d) => (
            <li
              key={d.id}
              className="flex items-center gap-2 py-0.5 text-sm"
              title={d.id}
            >
              <span className="font-mono text-xs text-zinc-500">
                {d.id.slice(0, 8)}
              </span>
              <StatusBadge status={d.status} />
              <button
                type="button"
                onClick={() => undeploy(d)}
                disabled={busy}
                aria-label="Undeploy"
                className="ml-auto rounded-md p-1 text-zinc-400 transition-colors hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
              >
                <Trash2 size={14} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
