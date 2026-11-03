"use client";

import { useMemo } from "react";
import type { DeployedTile } from "@/app/(session)/platform/DashboardTiles";
import RolloutModal from "@/app/components/integrations/RolloutModal";
import TestRunPanel from "@/app/components/upgrades/TestRunPanel";
import type { RolloutController } from "./useRollout";

/**
 * The rollout dialog for a page listing deployments of any integration, driven by
 * {@link useRollout}: pick a version, roll out or test & roll out, and follow the
 * test run. `deployments` is the list the page holds, which marks the target
 * integration's tags that are already live.
 */
export default function RolloutDialog({
  rollout,
  deployments,
}: {
  rollout: RolloutController;
  deployments: DeployedTile[];
}) {
  const target = rollout.target;
  const deployedTags = useMemo(() => {
    if (!target) return new Set<string>();
    return new Set(
      deployments
        .filter((d) => d.integrationId === target.integrationId)
        .map((d) => d.tag)
        .filter((t): t is string => Boolean(t)),
    );
  }, [deployments, target]);

  if (!target) return null;
  return (
    <RolloutModal
      integrationId={target.integrationId}
      integrationName={target.integrationName}
      deployments={[target]}
      snapshots={rollout.snapshots}
      deployedTags={deployedTags}
      versionMode="pick"
      busy={rollout.busy}
      error={rollout.error}
      onSubmit={rollout.submit}
      onTestSubmit={rollout.test}
      body={
        rollout.testRun && (
          <TestRunPanel
            key={rollout.testRun.id}
            initial={rollout.testRun}
            onFinished={rollout.testFinished}
            onClose={rollout.close}
          />
        )
      }
      onClose={() => {
        if (!rollout.busy) rollout.close();
      }}
    />
  );
}
