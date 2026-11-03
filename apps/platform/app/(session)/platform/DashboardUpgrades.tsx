"use client";

import { useMemo } from "react";
import { useRoles } from "@/app/auth/RolesContext";
import UpgradeNotice from "@/app/components/deployments/UpgradeNotice";
import RolloutDialog from "@/app/components/deployments/RolloutDialog";
import { useRollout } from "@/app/components/deployments/useRollout";
import type { DeployedTile } from "./DashboardTiles";

/**
 * The dashboard's word on runtime upgrades: which deployments run an older
 * runtime than this install deploys, each one a click from its rollout dialog.
 * Nothing renders while every deployment is current.
 */
export default function DashboardUpgrades({
  deployments,
  onChanged,
}: {
  deployments: DeployedTile[];
  /** Reload the dashboard's list once a rollout lands. */
  onChanged: () => Promise<void> | void;
}) {
  const { can } = useRoles();
  const rollout = useRollout(onChanged);
  const behind = useMemo(
    () => deployments.filter((d) => d.upgradeTo),
    [deployments],
  );
  if (behind.length === 0 && !rollout.target) return null;
  return (
    <>
      {behind.length > 0 && (
        <UpgradeNotice
          behind={behind}
          onRollOut={can.deploy ? rollout.open : undefined}
        />
      )}
      <RolloutDialog rollout={rollout} />
    </>
  );
}
