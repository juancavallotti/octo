"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Rocket } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  createDeployment,
  deleteDeployment,
  listDeployments,
  rolloutDeployment,
  scaleDeployment,
  type Deployment,
  type DeploymentInput,
  type Snapshot,
} from "@/app/model/orchestrator";
import DeploymentRow from "./DeploymentRow";
import DeployModal from "./DeployModal";

/**
 * Deployments for one integration: a one-click Deploy plus a list of live
 * deployments with their status and an Undeploy action. The orchestrator pushes
 * status changes over SSE (it watches the cluster), so the list updates live; if
 * the stream is unavailable we fall back to gentle polling. Mutations refresh
 * immediately, mirroring IntegrationsManager.
 */

// Polling cadence used only as a fallback when the SSE stream is unavailable.
const FALLBACK_POLL_MS = 5000;

// A concise option label for the selector bar: status, the version tag (or a
// short id for legacy deployments), and the ready/desired count.
function deploymentLabel(d: Deployment): string {
  const version = d.tag ?? d.id.slice(0, 8);
  return `${d.status} · ${version} · ${d.readyReplicas}/${d.desiredReplicas} ready`;
}

export default function DeploymentsSection({
  integrationId,
  integrationName,
  snapshots,
  onDeploymentsChange,
  onOpenLogs,
}: {
  integrationId: string;
  integrationName: string;
  /** The integration's tags (owned by the parent), for the change-version menu. */
  snapshots: Snapshot[];
  /**
   * Notifies the parent whenever the live deployment list changes, so it can tell
   * the Versions section which tags are deployed (and therefore undeletable).
   */
  onDeploymentsChange?: (deployments: Deployment[]) => void;
  /** Open the parent-owned dockable log panel tailing a specific pod. */
  onOpenLogs?: (deploymentId: string, podName: string) => void;
}) {
  const confirm = useConfirm();
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  // Which deployment's details are shown below the selector bar. Resolved against
  // the live list so it survives refreshes and falls back to the first deployment
  // when the selection goes away (derived, no correcting effect).
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const selected =
    deployments.find((d) => d.id === selectedId) ?? deployments[0] ?? null;

  // Mirror the live list up to the parent (for the Versions section's deployed-tag
  // hint). Effect-driven so it stays in sync regardless of which path — first
  // paint, SSE frame, or poll — last set the list.
  useEffect(() => {
    onDeploymentsChange?.(deployments);
  }, [deployments, onDeploymentsChange]);

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

  // Live updates over SSE, with a polling fallback that engages only while the
  // stream is erroring (e.g. orchestrator without informer support).
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  useEffect(() => {
    const stopPoll = () => {
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = null;
      }
    };
    refresh(); // first paint, independent of the stream connecting
    const es = new EventSource(
      `/api/integrations/${encodeURIComponent(integrationId)}/deployments/events`,
    );
    es.onmessage = (ev) => {
      try {
        setDeployments(JSON.parse(ev.data) as Deployment[]);
        setError(null);
      } catch {
        /* ignore a malformed frame; the next one replaces it */
      }
    };
    es.onopen = stopPoll; // stream healthy → no need to poll
    es.onerror = () => {
      // Stream dropped or unavailable; keep the list fresh until it recovers.
      if (!pollRef.current) {
        pollRef.current = setInterval(refresh, FALLBACK_POLL_MS);
      }
    };
    return () => {
      es.close();
      stopPoll();
    };
  }, [integrationId, refresh]);

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

  // Deploy from the modal: on success close it; on failure keep it open with the
  // error so the user can correct and retry.
  const deploy = useCallback(
    async (input: DeploymentInput) => {
      setBusy(true);
      setError(null);
      try {
        await createDeployment(integrationId, input);
        await refresh();
        setModalOpen(false);
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [integrationId, refresh],
  );

  const openModal = () => {
    setError(null);
    setModalOpen(true);
  };

  const scale = (d: Deployment, replicas: number) =>
    run(() => scaleDeployment(d.id, replicas));

  const rollout = (d: Deployment, snapshotId: string) =>
    run(() => rolloutDeployment(d.id, snapshotId));

  const undeploy = async (d: Deployment) => {
    const ok = await confirm({
      title: `Undeploy "${d.name}"?`,
      body: `Deployment ${d.id.slice(0, 8)} will be removed from the cluster.`,
      confirmLabel: "Undeploy",
      danger: true,
    });
    if (ok) run(() => deleteDeployment(d.id));
  };

  return (
    <>
      {/* Selector bar: pick which deployment to inspect; its details render below.
          The Deploy action sits alongside so it's always reachable. */}
      <div className="mb-2 flex gap-2">
        {deployments.length === 0 ? (
          <p className="flex-1 py-1 text-sm text-zinc-400">Not deployed.</p>
        ) : (
          <select
            value={selected?.id ?? ""}
            aria-label="Deployment"
            disabled={busy}
            onChange={(e) => setSelectedId(e.target.value)}
            className="min-w-0 flex-1 rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
          >
            {deployments.map((d) => (
              <option key={d.id} value={d.id}>
                {deploymentLabel(d)}
              </option>
            ))}
          </select>
        )}
        <button
          type="button"
          onClick={openModal}
          disabled={busy}
          className="inline-flex shrink-0 items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
        >
          <Rocket size={14} />
          Deploy
        </button>
      </div>

      {/* Errors from undeploy show inline; deploy errors show inside the modal. */}
      {error && !modalOpen && <p className="mb-2 text-sm text-red-500">{error}</p>}

      {selected && (
        <ul>
          <DeploymentRow
            key={selected.id}
            deployment={selected}
            busy={busy}
            snapshots={snapshots}
            onScale={scale}
            onRollout={rollout}
            onUndeploy={undeploy}
            onOpenLogs={
              onOpenLogs ? (d, pod) => onOpenLogs(d.id, pod) : undefined
            }
          />
        </ul>
      )}

      {modalOpen && (
        <DeployModal
          integrationId={integrationId}
          integrationName={integrationName}
          busy={busy}
          error={error}
          onSubmit={deploy}
          onClose={() => !busy && setModalOpen(false)}
        />
      )}
    </>
  );
}
