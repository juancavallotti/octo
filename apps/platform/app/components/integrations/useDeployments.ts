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
} from "@/app/model/orchestrator";
import type { DeploySubmit } from "./DeployModal";
import type { RolloutSubmit } from "./RolloutModal";

/**
 * Everything the deployments panel does that is not rendering: the live list, how
 * it stays live, and the four mutations that change it.
 *
 * "How it stays live" is the part worth having in one place. The orchestrator
 * watches the cluster and pushes status over SSE; a polling fallback engages only
 * while that stream is erroring, and stops again the moment it reconnects. That
 * arrangement is three callbacks and an interval ref, and it has nothing to do
 * with what the panel looks like.
 */

// Polling cadence used only as a fallback when the SSE stream is unavailable.
const FALLBACK_POLL_MS = 5000;

export interface DeploymentsController {
  /** Every deployment of this integration, newest status first from the stream. */
  deployments: Deployment[];
  /** A mutation is in flight; the panel disables its controls. */
  busy: boolean;
  /** Inline error for scale/undeploy, and one per modal so they cannot clobber
   *  each other — a failed deploy must not blank the rollout dialog's message. */
  error: string | null;
  deployError: string | null;
  rolloutError: string | null;
  /** Clearing a modal's error is the dialog's own business — it does it on open and
   *  on close, which the hook has no way to observe. */
  setDeployError: (message: string | null) => void;
  setRolloutError: (message: string | null) => void;
  /** The deployment whose rollout dialog is open, if any. */
  rolloutTarget: Deployment | null;
  setRolloutTarget: (d: Deployment | null) => void;
  refresh: () => Promise<void>;
  deploy: (input: DeploySubmit) => Promise<void>;
  rollout: (input: RolloutSubmit) => Promise<void>;
  scale: (d: Deployment, replicas: number) => Promise<void>;
  undeploy: (d: Deployment) => Promise<void>;
}

export function useDeployments({
  integrationId,
  onDeployOpenChange,
  onSnapshotsChanged,
}: {
  integrationId: string;
  onDeployOpenChange: (open: boolean) => void;
  onSnapshotsChanged?: () => void;
}): DeploymentsController {
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
          ...(input.tracing ? { tracing: true } : {}),
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
        await rolloutDeployment(
          input.deploymentId,
          snapshotId,
          input.env,
          input.tracing,
        );
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
  return {
    deployments,
    busy,
    error,
    deployError,
    rolloutError,
    setDeployError,
    setRolloutError,
    rolloutTarget,
    setRolloutTarget,
    refresh,
    deploy,
    rollout,
    scale,
    undeploy,
  };
}
