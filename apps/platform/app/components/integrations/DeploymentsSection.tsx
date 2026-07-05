"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  createDeployment,
  createSnapshot,
  deleteDeployment,
  listDeployments,
  rolloutDeployment,
  scaleDeployment,
  type Deployment,
  type Snapshot,
} from "@/app/model/orchestrator";
import DeploymentRow from "./DeploymentRow";
import DeployModal, { type DeploySubmit } from "./DeployModal";
import RolloutModal, { type RolloutSubmit } from "./RolloutModal";

/**
 * Deployments for one integration, scoped to the active version: with a tag
 * selected it lists that tag's deployments; with Current selected it lists them
 * all. Each row shows live status and its actions (scale, rollout, undeploy, pod
 * logs). The orchestrator pushes status over SSE (it watches the cluster), so the
 * list updates live; if the stream is unavailable we fall back to gentle polling.
 * The Deploy button lives in the parent header and controls this modal.
 */

// Polling cadence used only as a fallback when the SSE stream is unavailable.
const FALLBACK_POLL_MS = 5000;

export default function DeploymentsSection({
  integrationId,
  integrationName,
  snapshots,
  activeSnapshot,
  filterTag,
  deployOpen,
  onDeployOpenChange,
  onDeploymentsChange,
  onSnapshotsChanged,
  onOpenLogs,
}: {
  integrationId: string;
  integrationName: string;
  /** The integration's tags (owned by the parent), for the change-version menu. */
  snapshots: Snapshot[];
  /** The active version to deploy: a frozen tag, or null to tag-and-deploy Current. */
  activeSnapshot: Snapshot | null;
  /** Show only this tag's deployments; null (Current) shows them all. */
  filterTag: string | null;
  /** Whether the Deploy modal is open — controlled by the parent, whose header
   * hosts the Deploy button. */
  deployOpen: boolean;
  onDeployOpenChange: (open: boolean) => void;
  /**
   * Notifies the parent whenever the live deployment list changes, so it can tell
   * the version pills which tags are deployed (and therefore undeletable).
   */
  onDeploymentsChange?: (deployments: Deployment[]) => void;
  /** Ask the parent to reload the tag list (after a tag-on-deploy creates one). */
  onSnapshotsChanged?: () => void;
  /** Open the parent-owned dockable log panel tailing a specific pod. */
  onOpenLogs?: (deploymentId: string, podName: string) => void;
}) {
  const confirm = useConfirm();
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [busy, setBusy] = useState(false);
  // Separate error slots so they don't clobber each other: `error` is the inline
  // scale/undeploy error; `deployError` shows in the Deploy modal; `rolloutError` in
  // the Rollout modal.
  const [error, setError] = useState<string | null>(null);
  const [deployError, setDeployError] = useState<string | null>(null);
  const [rolloutError, setRolloutError] = useState<string | null>(null);
  // The deployment whose rollout dialog is open (change version and/or edit env).
  const [rolloutTarget, setRolloutTarget] = useState<Deployment | null>(null);
  // Scope the list to the active version: a tag shows only its deployments; Current
  // (null) shows them all so everything is manageable in one place.
  const shown =
    filterTag === null
      ? deployments
      : deployments.filter((d) => d.tag === filterTag);

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
  // error so the user can correct and retry. When deploying Current the modal sends
  // a newTag — cut that snapshot from the working copy first, then deploy it (and
  // tell the parent so the pills/menu pick up the new tag).
  const deploy = useCallback(
    async (input: DeploySubmit) => {
      setBusy(true);
      setDeployError(null);
      try {
        let snapshotId = input.snapshotId;
        if (!snapshotId && input.newTag) {
          const snap = await createSnapshot(integrationId, input.newTag);
          snapshotId = snap.id;
          onSnapshotsChanged?.();
        }
        if (!snapshotId) throw new Error("no version selected to deploy");
        await createDeployment(integrationId, {
          snapshotId,
          replicas: input.replicas,
          ...(input.slug ? { slug: input.slug } : {}),
          ...(input.expose ? { expose: input.expose } : {}),
          ...(input.env ? { env: input.env } : {}),
        });
        await refresh();
        onDeployOpenChange(false);
      } catch (e) {
        setDeployError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [integrationId, refresh, onDeployOpenChange, onSnapshotsChanged],
  );

  const scale = (d: Deployment, replicas: number) =>
    run(() => scaleDeployment(d.id, replicas));

  // Roll a deployment over from its dialog: an existing tag deploys directly; a new
  // tag is cut from the working copy first (then the pills/menu pick it up). The
  // dialog's env replaces the deployment's stored bindings. Keep the modal open on
  // failure so the operator can correct and retry.
  const rollout = useCallback(
    async (input: RolloutSubmit) => {
      setBusy(true);
      setRolloutError(null);
      try {
        let snapshotId = input.snapshotId;
        if (!snapshotId && input.newTag) {
          const snap = await createSnapshot(integrationId, input.newTag);
          snapshotId = snap.id;
          onSnapshotsChanged?.();
        }
        if (!snapshotId) throw new Error("no version selected to roll out");
        await rolloutDeployment(input.deploymentId, snapshotId, input.env);
        await refresh();
        setRolloutTarget(null);
      } catch (e) {
        setRolloutError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [integrationId, refresh, onSnapshotsChanged],
  );

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
      {/* Scale/rollout/undeploy errors show inline; deploy errors show in the modal. */}
      {error && <p className="mb-2 text-sm text-red-500">{error}</p>}

      {shown.length === 0 ? (
        <p className="py-1 text-sm text-zinc-400">
          {filterTag === null
            ? "Not deployed."
            : `No deployments of ${filterTag}.`}
        </p>
      ) : (
        <ul className="space-y-2">
          {shown.map((d) => (
            <DeploymentRow
              key={d.id}
              deployment={d}
              busy={busy}
              onScale={scale}
              onOpenRollout={(dep) => {
                setRolloutError(null);
                setRolloutTarget(dep);
              }}
              onUndeploy={undeploy}
              onOpenLogs={
                onOpenLogs ? (dep, pod) => onOpenLogs(dep.id, pod) : undefined
              }
            />
          ))}
        </ul>
      )}

      {deployOpen && (
        <DeployModal
          integrationId={integrationId}
          integrationName={integrationName}
          activeSnapshot={activeSnapshot}
          snapshots={snapshots}
          busy={busy}
          error={deployError}
          onSubmit={deploy}
          onClose={() => {
            if (busy) return;
            setDeployError(null); // start fresh next time it opens
            onDeployOpenChange(false);
          }}
        />
      )}

      {rolloutTarget && (
        <RolloutModal
          integrationId={integrationId}
          integrationName={integrationName}
          deployments={[rolloutTarget]}
          snapshots={snapshots}
          deployedTags={
            new Set(
              deployments
                .map((d) => d.tag)
                .filter((t): t is string => Boolean(t)),
            )
          }
          versionMode="pick"
          busy={busy}
          error={rolloutError}
          onSubmit={rollout}
          onClose={() => {
            if (busy) return;
            setRolloutError(null);
            setRolloutTarget(null);
          }}
        />
      )}
    </>
  );
}
