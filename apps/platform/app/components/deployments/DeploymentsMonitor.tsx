"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { FolderTree, RefreshCw } from "lucide-react";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import { listAllDeployments, scaleDeployment } from "@/app/model/orchestrator";
import {
  EmptyState,
  type DeployedTile,
} from "@/app/(session)/platform/DashboardTiles";
import PodLogPanel from "@/app/components/integrations/PodLogPanel";
import RolloutModal from "@/app/components/integrations/RolloutModal";
import DeploymentListItem from "./DeploymentListItem";
import UpgradeNotice from "./UpgradeNotice";
import { useRollout } from "./useRollout";
import { needsUpgrade } from "@/app/lib/runtimeRelease";

/**
 * The deployments view: every active deployment across every integration, shown as
 * a card grid with live per-pod state (polled while the page is open) — two columns
 * where the viewport is wide enough for them, one below that. Unlike the dashboard's
 * read-only tile grid, each card exposes the management actions the dedicated page
 * is for — scale it, roll it over to another version (the same dialog that turns
 * tracing on and off), tail one pod's logs, or open the integration in the
 * manager/editor. Reuses the dashboard's aggregation
 * (listAllDeployments) so the page and the dashboard summary stay in sync, and the
 * integrations panel's rollout dialog and log panel so a deployment is operated
 * the same way wherever it is met.
 *
 * It also answers the question a fleet raises that a single deployment does not:
 * which of these are running an older runtime than a deploy made today would get.
 * A deployment keeps the image it was created with until somebody rolls it over,
 * so on a cluster that has been upgraded a few times the answer is scattered
 * across the grid — which is why it is also stated once, at the top.
 */
export default function DeploymentsMonitor({
  currentRuntime = "",
}: {
  /**
   * The octo runtime this install deploys now, from the page's server-only env.
   * Empty means it has no way of knowing, and nothing is claimed about any
   * deployment's runtime being old.
   */
  currentRuntime?: string;
}) {
  const { available, ready } = useOrchestrator();
  const [deployments, setDeployments] = useState<DeployedTile[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [busy, setBusy] = useState(false);

  // Fetch without touching the spinner flag, so it's safe to call from the effect.
  const load = useCallback(
    () =>
      listAllDeployments().then(
        (ds) => {
          setDeployments(ds);
          setError(null);
        },
        (e) => setError((e as Error).message),
      ),
    [],
  );

  const refresh = useCallback(() => {
    setRefreshing(true);
    load().finally(() => setRefreshing(false));
  }, [load]);

  // Initial load plus light polling so deployment status stays current.
  useEffect(() => {
    if (!available) return;
    load();
    const id = setInterval(load, 8000);
    return () => clearInterval(id);
  }, [available, load]);

  // Scale a deployment, then refresh so the count reflects the live desired state.
  const scale = useCallback(
    async (d: DeployedTile, replicas: number) => {
      setBusy(true);
      setError(null);
      try {
        await scaleDeployment(d.id, replicas);
        await load();
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [load],
  );

  const rollout = useRollout(load);

  // The pod whose logs are docked at the bottom of the page, or null when closed.
  const [logsPod, setLogsPod] = useState<{
    deploymentId: string;
    podName: string;
  } | null>(null);

  const sorted = useMemo(
    () =>
      [...(deployments ?? [])].sort((a, b) =>
        a.integrationName.localeCompare(b.integrationName),
      ),
    [deployments],
  );

  // Which deployments a deploy made today would put on a newer runtime. Derived
  // rather than held: it is a reading of the list that has just been polled.
  const behind = useMemo(
    () => sorted.filter((d) => needsUpgrade(d.runtimeVersion, currentRuntime)),
    [sorted, currentRuntime],
  );

  // The tags this integration already has running, so the version picker can mark
  // them — the same hint the integrations panel gives, derived from the list this
  // page has loaded anyway.
  const deployedTags = useMemo(() => {
    const target = rollout.target;
    if (!target) return new Set<string>();
    return new Set(
      (deployments ?? [])
        .filter((d) => d.integrationId === target.integrationId)
        .map((d) => d.tag)
        .filter((t): t is string => Boolean(t)),
    );
  }, [deployments, rollout.target]);

  // A column, not a single scroll area: the log panel docks under the list at a
  // fixed height, so the list has to be the part that scrolls.
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-6xl px-6 py-8">
          <div className="flex items-center gap-2">
            <h1 className="text-xl font-semibold tracking-tight">
              Deployments
            </h1>
            {sorted.length > 0 && (
              <span className="rounded-full bg-black/[0.06] px-2 py-0.5 text-xs font-medium text-zinc-500 dark:bg-white/[0.08] dark:text-zinc-400">
                {sorted.length}
              </span>
            )}
            {available && (
              <button
                type="button"
                onClick={refresh}
                disabled={refreshing}
                aria-label="Refresh deployments"
                className="ml-1 rounded-md p-1 text-zinc-400 transition-colors hover:bg-black/[0.05] hover:text-zinc-700 disabled:opacity-50 dark:hover:bg-white/[0.06] dark:hover:text-zinc-200"
              >
                <RefreshCw
                  size={14}
                  className={refreshing ? "animate-spin" : undefined}
                />
              </button>
            )}
          </div>
          <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
            Every active deployment across your integrations, with live status.
          </p>

          {error && (
            <p className="mt-4 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-sm text-red-500">
              {error}
            </p>
          )}

          {behind.length > 0 && (
            <UpgradeNotice behind={behind} currentRuntime={currentRuntime} onRollOut={rollout.open} />
          )}

          <div className="mt-6">
            {!ready ? null : !available ? (
              <EmptyState
                icon={FolderTree}
                title="Deployments unavailable"
                body="Set ORCHESTRATOR_URL to connect this editor to a cluster."
              />
            ) : deployments === null ? (
              <p className="text-sm text-zinc-400">Loading deployments…</p>
            ) : sorted.length === 0 ? (
              <EmptyState
                icon={FolderTree}
                title="No deployments yet"
                body="Deploy an integration and it will show up here with live status."
              />
            ) : (
              <ul className="grid grid-cols-1 gap-3 xl:grid-cols-2">
                {sorted.map((d) => (
                  <DeploymentListItem
                    key={d.id}
                    deployment={d}
                    busy={busy || rollout.busy}
                    currentRuntime={currentRuntime}
                    onScale={scale}
                    onOpenRollout={rollout.open}
                    onOpenLogs={(dep, podName) =>
                      setLogsPod({ deploymentId: dep.id, podName })
                    }
                  />
                ))}
              </ul>
            )}
          </div>
        </div>
      </div>

      {rollout.target && (
        <RolloutModal
          integrationId={rollout.target.integrationId}
          integrationName={rollout.target.integrationName}
          deployments={[rollout.target]}
          snapshots={rollout.snapshots}
          deployedTags={deployedTags}
          versionMode="pick"
          busy={rollout.busy}
          error={rollout.error}
          onSubmit={rollout.submit}
          onClose={() => {
            if (!rollout.busy) rollout.close();
          }}
        />
      )}

      {/* Docked pod-log panel, keyed by pod so switching pods resets the stream. */}
      {logsPod && (
        <PodLogPanel
          key={`${logsPod.deploymentId}:${logsPod.podName}`}
          deploymentId={logsPod.deploymentId}
          podName={logsPod.podName}
          onClose={() => setLogsPod(null)}
        />
      )}
    </div>
  );
}
