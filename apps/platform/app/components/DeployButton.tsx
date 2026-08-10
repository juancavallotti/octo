"use client";

import { useState } from "react";
import { Rocket } from "lucide-react";
import { useSave } from "@octo/editor";
import {
  createDeployment,
  createSnapshot,
  getIntegration,
  listDeployments,
  listSnapshots,
  rolloutDeployment,
  type Deployment,
  type Snapshot,
} from "@/app/model/orchestrator";
import DeployModal, { type DeploySubmit } from "./integrations/DeployModal";
import RolloutModal, {
  type RolloutSubmit,
} from "./integrations/RolloutModal";

/**
 * Editor-header control that ships the current integration in one step. Clicking it
 * saves the on-screen definition, then opens a tag + environment dialog:
 *
 *  - with live deployment(s) → the rollout dialog (new-tag mode): the operator picks
 *    which deployment to upgrade, names the new version, and edits its environment
 *    (seeded from that deployment). It tags the working copy and rolls that one over.
 *  - with nothing deployed yet → the first-deploy modal (Current mode): name the
 *    version, set scale/address/env, then it tags and deploys.
 *
 * Renders nothing without a filesystem capability and is disabled on an empty
 * document, mirroring {@link TagButton}. The authoritative id is read from
 * `getIntegrationId` (a ref the host updates on save) after saving.
 */
export default function DeployButton({
  getIntegrationId,
}: {
  getIntegrationId: () => string | null;
}) {
  const save = useSave();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // The rollout dialog's context (live deployments to choose among). Null when closed.
  const [rollout, setRollout] = useState<{
    id: string;
    name: string;
    deployments: Deployment[];
    snapshots: Snapshot[];
  } | null>(null);
  // The first-deploy dialog's context (nothing live yet). Null when closed.
  const [firstDeploy, setFirstDeploy] = useState<{
    id: string;
    name: string;
    snapshots: Snapshot[];
  } | null>(null);

  // No filesystem capability => nothing to deploy (mirrors how Save hides).
  if (!save) return null;

  // Save the on-screen definition, then open the right dialog: rollout when the
  // integration has live deployments, first-deploy otherwise. Saving first ensures a
  // tag cut later freezes what's on screen, and that the dialog's deploy options
  // (declared env, networked-ness) reflect the current definition.
  const begin = async () => {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await save.save();
      const id = getIntegrationId();
      if (!id) {
        setError("Save the integration before deploying.");
        return;
      }
      const [integration, deployments, snapshots] = await Promise.all([
        getIntegration(id),
        listDeployments(id),
        listSnapshots(id),
      ]);
      if (deployments.length > 0) {
        setRollout({ id, name: integration.name, deployments, snapshots });
      } else {
        setFirstDeploy({ id, name: integration.name, snapshots });
      }
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  // Roll the chosen deployment over to a freshly tagged version, with the edited env.
  const submitRollout = async (input: RolloutSubmit) => {
    if (!rollout || !input.newTag) return;
    setBusy(true);
    setError(null);
    try {
      const snap = await createSnapshot(rollout.id, input.newTag);
      await rolloutDeployment(input.deploymentId, snap.id, input.env, input.tracing);
      setRollout(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  // First deploy: tag the working copy, then ship it as a new workload.
  const submitFirstDeploy = async (input: DeploySubmit) => {
    if (!firstDeploy || !input.newTag) return;
    setBusy(true);
    setError(null);
    try {
      const snap = await createSnapshot(firstDeploy.id, input.newTag);
      await createDeployment(firstDeploy.id, {
        snapshotId: snap.id,
        replicas: input.replicas,
        ...(input.slug ? { slug: input.slug } : {}),
        ...(input.expose ? { expose: input.expose } : {}),
        ...(input.env ? { env: input.env } : {}),
        ...(input.tracing ? { tracing: true } : {}),
      });
      setFirstDeploy(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="relative">
      <button
        type="button"
        onClick={begin}
        disabled={save.empty || busy}
        title={save.empty ? "Nothing to deploy yet" : "Deploy this integration"}
        className="inline-flex items-center gap-1.5 rounded-md bg-emerald-600 px-2.5 py-1 text-sm font-medium text-white transition-colors hover:bg-emerald-500 disabled:opacity-50"
      >
        <Rocket size={14} />
        {busy && !rollout && !firstDeploy ? "Preparing…" : "Deploy"}
      </button>

      {error && !rollout && !firstDeploy && (
        <p className="absolute right-0 top-full z-30 mt-1 w-56 rounded-md border border-red-500/30 bg-white px-2 py-1 text-xs text-red-500 shadow-lg dark:bg-zinc-900">
          {error}
        </p>
      )}

      {rollout && (
        <RolloutModal
          integrationId={rollout.id}
          integrationName={rollout.name}
          deployments={rollout.deployments}
          snapshots={rollout.snapshots}
          deployedTags={
            new Set(
              rollout.deployments
                .map((d) => d.tag)
                .filter((t): t is string => Boolean(t)),
            )
          }
          versionMode="new-tag"
          actionLabel="Deploy"
          busy={busy}
          error={error}
          onSubmit={submitRollout}
          onClose={() => {
            if (busy) return;
            setError(null);
            setRollout(null);
          }}
        />
      )}

      {firstDeploy && (
        <DeployModal
          integrationId={firstDeploy.id}
          integrationName={firstDeploy.name}
          activeSnapshot={null}
          snapshots={firstDeploy.snapshots}
          busy={busy}
          error={error}
          onSubmit={submitFirstDeploy}
          onClose={() => {
            if (busy) return;
            setError(null);
            setFirstDeploy(null);
          }}
        />
      )}
    </div>
  );
}
