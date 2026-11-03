"use client";

import { useCallback, useRef, useState } from "react";
import {
  createSnapshot,
  listSnapshots,
  rolloutDeployment,
  type Snapshot,
} from "@/app/model/orchestrator";
import { startTestRun, type TestRun } from "@/app/model/upgrades";
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
  /** The target integration's tags; null until the fetch lands. */
  snapshots: Snapshot[] | null;
  busy: boolean;
  error: string | null;
  /** The test run "Test & roll out" started, which the dialog follows; null before. */
  testRun: TestRun | null;
  open: (d: DeployedTile) => void;
  close: () => void;
  submit: (input: RolloutSubmit) => Promise<void>;
  /** Run the target version's suites, rolling the deployment out when they pass. */
  test: (input: RolloutSubmit) => Promise<void>;
  /** Refresh once the followed test run finishes, since it may have rolled out. */
  testFinished: () => void;
}

export function useRollout(
  /** Refresh the list once a rollout lands, so the row shows its new state. */
  onDone: () => Promise<void> | void,
): RolloutController {
  const [target, setTarget] = useState<DeployedTile | null>(null);
  const [snapshots, setSnapshots] = useState<Snapshot[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [testRun, setTestRun] = useState<TestRun | null>(null);
  // Which fetch the dialog is waiting on. Opening or closing invalidates the one
  // before it, because the tags of a deployment nobody is looking at any more
  // must not land in the dialog of the one they are: two integrations' versions
  // are indistinguishable once they are in the picker, and rolling out to the
  // wrong one would succeed.
  const request = useRef(0);

  const open = useCallback((d: DeployedTile) => {
    const mine = ++request.current;
    setError(null);
    setSnapshots(null);
    setTestRun(null);
    setTarget(d);
    listSnapshots(d.integrationId).then(
      (tags) => {
        if (mine === request.current) setSnapshots(tags);
      },
      (e) => {
        if (mine === request.current) setError((e as Error).message);
      },
    );
  }, []);

  const close = useCallback(() => {
    request.current += 1;
    setError(null);
    setTestRun(null);
    setTarget(null);
  }, []);

  // The version to act on: an existing tag as it is, a new one cut from the
  // working copy first.
  const resolveSnapshot = useCallback(
    async (input: RolloutSubmit): Promise<string> => {
      if (input.snapshotId) return input.snapshotId;
      if (target && input.newTag) {
        const snap = await createSnapshot(target.integrationId, input.newTag);
        return snap.id;
      }
      throw new Error("no version selected to roll out");
    },
    [target],
  );

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
        const snapshotId = await resolveSnapshot(input);
        await rolloutDeployment(input.deploymentId, {
          snapshotId,
          env: input.env,
          tracing: input.tracing,
          runtimeVersion: input.runtimeVersion,
        });
        await onDone();
        setTarget(null);
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [target, onDone, resolveSnapshot],
  );

  // The orchestrator runs the suites and makes the rollout itself, with the
  // deployment's saved env and tracing; the dialog follows the run.
  const test = useCallback(
    async (input: RolloutSubmit) => {
      if (!target) return;
      setBusy(true);
      setError(null);
      try {
        const snapshotId = await resolveSnapshot(input);
        setTestRun(
          await startTestRun(input.deploymentId, {
            snapshotId,
            rolloutOnPass: true,
          }),
        );
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [target, resolveSnapshot],
  );

  const testFinished = useCallback(() => {
    void onDone();
  }, [onDone]);

  return {
    target,
    snapshots,
    busy,
    error,
    testRun,
    open,
    close,
    submit,
    test,
    testFinished,
  };
}
