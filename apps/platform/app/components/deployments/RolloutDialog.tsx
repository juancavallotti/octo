"use client";

import RolloutModal from "@/app/components/integrations/RolloutModal";
import TestRunPanel from "@/app/components/upgrades/TestRunPanel";
import type { RolloutController } from "./useRollout";

/**
 * The rollout dialog for a page listing deployments of any integration, driven by
 * {@link useRollout}: pick a version, roll out or test & roll out, and follow the
 * test run.
 */
export default function RolloutDialog({
  rollout,
}: {
  rollout: RolloutController;
}) {
  const target = rollout.target;

  if (!target) return null;
  return (
    <RolloutModal
      integrationId={target.integrationId}
      integrationName={target.integrationName}
      deployments={[target]}
      snapshots={rollout.snapshots}
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
            editorHref={`/platform/i/${encodeURIComponent(target.integrationId)}`}
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
