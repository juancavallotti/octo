"use client";

import type {
  Deployment,
  Integration,
  IntegrationDetailData,
  Snapshot,
} from "@/app/model/orchestrator";
import { Section } from "./DetailLayout";
import DeploymentsSection from "./DeploymentsSection";
import EnvSection from "./EnvSection";
import ResourcesSection from "./ResourcesSection";

/**
 * The cards that operate on a version: its deployments, its resources and its
 * declared env.
 *
 * They mount once the pane's first read has settled, each handed its part of it
 * so none makes a read of its own to start with. A part that could not be read
 * is handed as nothing, and that section reads it itself.
 */
export default function OperatingSections({
  integration,
  detail,
  snapshots,
  activeSnapshot,
  effectiveTag,
  deployOpen,
  onDeployOpenChange,
  onDeploymentsChange,
  onSnapshotsChanged,
  onOpenLogs,
}: {
  integration: Integration;
  /** The pane's first read; null while it is out. */
  detail: IntegrationDetailData | null;
  snapshots: Snapshot[];
  /** The tag in view, or null for the working copy. */
  activeSnapshot: Snapshot | null;
  effectiveTag: string | null;
  deployOpen: boolean;
  onDeployOpenChange: (open: boolean) => void;
  onDeploymentsChange: (deployments: Deployment[]) => void;
  onSnapshotsChanged: () => void;
  onOpenLogs: (deploymentId: string, podName: string) => void;
}) {
  if (detail === null) {
    return (
      <>
        {["Deployments", "Resources", "Env"].map((title) => (
          <Section key={title} title={title}>
            <p className="text-sm text-zinc-400">Loading…</p>
          </Section>
        ))}
      </>
    );
  }

  return (
    <>
      <Section title="Deployments">
        {/* Keyed by integration id so switching selection resets its state. */}
        <DeploymentsSection
          key={integration.id}
          integrationId={integration.id}
          initial={detail.deployments ?? undefined}
          integrationName={integration.name}
          snapshots={snapshots}
          activeSnapshot={activeSnapshot}
          filterTag={effectiveTag}
          deployOpen={deployOpen}
          onDeployOpenChange={onDeployOpenChange}
          onDeploymentsChange={onDeploymentsChange}
          onSnapshotsChanged={onSnapshotsChanged}
          onOpenLogs={onOpenLogs}
        />
      </Section>

      <Section title="Resources">
        {/* Scoped to the active version: the live working copy, or a tag's
            frozen (read-only) set. */}
        <ResourcesSection
          key={integration.id}
          integrationId={integration.id}
          initial={detail.resources ?? undefined}
          snapshotId={activeSnapshot?.id}
          versionLabel={effectiveTag ?? undefined}
        />
      </Section>

      <Section title="Env">
        {/* Read-only declared env for the active version (values are set in
            the Deploy modal). Scoped to the same version as Resources. */}
        <EnvSection
          key={integration.id}
          integrationId={integration.id}
          initial={detail.envVars ?? undefined}
          snapshotId={activeSnapshot?.id}
        />
      </Section>
    </>
  );
}
