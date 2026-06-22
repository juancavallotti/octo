"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  FileText,
  FolderTree,
  KeyRound,
  LayoutGrid,
  Plus,
  RefreshCw,
} from "lucide-react";
import AppHeader from "@/app/components/AppHeader";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import { listDeployments, listIntegrations } from "@/app/model/orchestrator";
import {
  DeploymentTile,
  EmptyState,
  ShortcutTile,
  type DeployedTile,
} from "./DashboardTiles";

/** Pull every deployment across every integration into one flat, named list. */
async function loadDeployments(): Promise<DeployedTile[]> {
  const integrations = await listIntegrations();
  const lists = await Promise.all(
    integrations.map((i) =>
      listDeployments(i.id).then(
        (ds) => ds.map((d) => ({ ...d, integrationName: i.name })),
        () => [] as DeployedTile[],
      ),
    ),
  );
  return lists.flat();
}

/**
 * The platform dashboard: shortcut tiles for the common actions, then a live grid
 * of every deployed integration and its status (polled while the page is open).
 * The shared AppHeader carries the logo and account tile; the logo is inert here
 * since this is already the dashboard.
 */
export default function Dashboard({ userMenu }: { userMenu?: React.ReactNode }) {
  const { available, ready } = useOrchestrator();
  const [deployments, setDeployments] = useState<DeployedTile[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  // Fetch without touching the spinner flag, so it's safe to call from the effect
  // (its setState calls run inside the promise callbacks, not synchronously).
  const load = useCallback(
    () =>
      loadDeployments().then(
        (ds) => {
          setDeployments(ds);
          setError(null);
        },
        (e) => setError((e as Error).message),
      ),
    [],
  );

  /** Manual refresh shows a spinner (event handlers may setState freely). */
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

  const sorted = useMemo(
    () =>
      [...(deployments ?? [])].sort((a, b) =>
        a.integrationName.localeCompare(b.integrationName),
      ),
    [deployments],
  );

  return (
    <div className="flex h-full flex-col">
      <AppHeader logoHref={null} userMenu={userMenu} />

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-6xl px-6 py-8">
          <h1 className="text-xl font-semibold tracking-tight">Dashboard</h1>
          <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
            Your integrations at a glance.
          </p>

          {/* Shortcuts */}
          <div className="mt-6 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <ShortcutTile
              href="/platform/new"
              icon={Plus}
              title="New integration"
              subtitle="Start from a blank canvas"
              accent
            />
            <ShortcutTile
              href="/platform/integrations"
              icon={LayoutGrid}
              title="Integrations"
              subtitle="Browse, organize, deploy"
            />
            <ShortcutTile
              href="/platform/integrations?view=secrets"
              icon={KeyRound}
              title="Secrets"
              subtitle="Manage deploy-time secrets"
            />
            <ShortcutTile
              href="https://juancavallotti.github.io/octo/"
              icon={FileText}
              title="Documentation"
              subtitle="Guides and patterns"
              external
            />
          </div>

          {/* Deployments */}
          <div className="mt-10 flex items-center gap-2">
            <h2 className="text-sm font-semibold uppercase tracking-wide text-zinc-400">
              Deployments
            </h2>
            {available && (
              <button
                type="button"
                onClick={refresh}
                disabled={refreshing}
                aria-label="Refresh deployments"
                className="rounded-md p-1 text-zinc-400 transition-colors hover:bg-black/[0.05] hover:text-zinc-700 disabled:opacity-50 dark:hover:bg-white/[0.06] dark:hover:text-zinc-200"
              >
                <RefreshCw
                  size={14}
                  className={refreshing ? "animate-spin" : undefined}
                />
              </button>
            )}
          </div>

          {error && (
            <p className="mt-3 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-sm text-red-500">
              {error}
            </p>
          )}

          <div className="mt-4">
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
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {sorted.map((d) => (
                  <DeploymentTile key={d.id} d={d} />
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
