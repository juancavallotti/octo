"use client";

import { useEffect } from "react";
import type { Deployment, Snapshot } from "@/app/model/orchestrator";
import { useDeployments } from "./useDeployments";
import DeploymentRow from "./DeploymentRow";
import DeployModal from "./DeployModal";
import RolloutModal from "./RolloutModal";

/**
 * Deployments for one integration, scoped to the active version: with a tag
 * selected it lists that tag's deployments; with Current selected it lists them
 * all. Each row shows live status and its actions (scale, rollout, undeploy, pod
 * logs). The orchestrator pushes status over SSE (it watches the cluster), so the
 * list updates live; if the stream is unavailable we fall back to gentle polling.
 * The Deploy button lives in the parent header and controls this modal.
 */

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
  const {
    deployments,
    busy,
    error,
    deployError,
    rolloutError,
    setDeployError,
    setRolloutError,
    rolloutTarget,
    setRolloutTarget,
    deploy,
    rollout,
    scale,
    undeploy,
  } = useDeployments({ integrationId, onDeployOpenChange, onSnapshotsChanged });

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
