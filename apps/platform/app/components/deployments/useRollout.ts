"use client";

import { useCallback, useState } from "react";
import {
  createSnapshot,
  listSnapshots,
  rolloutDeployment,
  type Snapshot,
} from "@/app/model/orchestrator";
import type { DeployedTile } from "@/app/(session)/platform/DashboardTiles";
import type { RolloutSubmit } from "@/app/components/integrations/RolloutModal";

/**
 * The rollout flow for the deployments page, where a row can belong to any
 * integration.
 *
 * The integrations panel already holds one integration's tags — its version pills
 * need them — so its rollout dialog opens with them in hand. This page spans every
 * integration and holds none, so opening the dialog is also what fetches the
 * target's versions. That is the whole reason this is a hook of its own rather
 * than the panel's: loading tags for every integration on a page where at most one
 * of them is about to be rolled out would be a request per row, every refresh.
 */
export interface RolloutController {
  /** The deployment whose dialog is open, or null. */
  target: DeployedTile | null;
  /** The target integration's tags; empty until the fetch lands. */
  snapshots: Snapshot[];
  busy: boolean;
  error: string | null;
  open: (d: DeployedTile) => void;
  close: () => void;
  submit: (input: RolloutSubmit) => Promise<void>;
}

export function useRollout(
  /** Refresh the list once a rollout lands, so the row shows its new state. */
  onDone: () => Promise<void> | void,
): RolloutController {
  const [target, setTarget] = useState<DeployedTile | null>(null);
  const [snapshots, setSnapshots] = useState<Snapshot[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const open = useCallback((d: DeployedTile) => {
    setError(null);
    setSnapshots([]);
    setTarget(d);
    listSnapshots(d.integrationId).then(setSnapshots, (e) =>
      setError((e as Error).message),
    );
  }, []);

  const close = useCallback(() => {
    setError(null);
    setTarget(null);
  }, []);

  // Mirrors the integrations panel's rollout: an existing tag rolls straight over,
  // a new one is cut from the working copy first. Failure keeps the dialog open
  // with its message, so the operator can correct and retry rather than lose the
  // env they just edited.
  const submit = useCallback(
    async (input: RolloutSubmit) => {
      if (!target) return;
      setBusy(true);
      setError(null);
      try {
        let snapshotId = input.snapshotId;
        if (!snapshotId && input.newTag) {
          const snap = await createSnapshot(target.integrationId, input.newTag);
          snapshotId = snap.id;
        }
        if (!snapshotId) throw new Error("no version selected to roll out");
        await rolloutDeployment(
          input.deploymentId,
          snapshotId,
          input.env,
          input.tracing,
        );
        await onDone();
        setTarget(null);
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [target, onDone],
  );

  return { target, snapshots, busy, error, open, close, submit };
}
