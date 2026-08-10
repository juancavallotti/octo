"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  listSnapshots,
  type Deployment,
  type Integration,
  type Snapshot,
} from "@/app/model/orchestrator";
import DefinitionSection from "./DefinitionSection";
import DeploymentsSection from "./DeploymentsSection";
import EnvSection from "./EnvSection";
import PodLogPanel from "./PodLogPanel";
import ResourcesSection from "./ResourcesSection";
import VersionPills from "./VersionPills";
import { Row, Section } from "./DetailLayout";
import IntegrationHeader from "./IntegrationHeader";

/**
 * Read-only operating details for the selected integration, plus its primary
 * actions (open in the editor, move to a folder, delete). Laid out as a list of
 * labelled sections so new operating data — run status, metrics, history — can be
 * added later as additional sections without reworking the layout.
 */

interface FlatFolder {
  id: string;
  name: string;
  parentId: string | null;
}

interface Props {
  integration: Integration;
  /** Flattened folders, used to render the current folder's path. */
  folders: FlatFolder[];
  /** The folder the integration currently belongs to, or null when unfiled. */
  folderId: string | null;
  busy: boolean;
  onDelete: () => void;
  /** Duplicate this integration into a new "Copy of …" record. */
  onCopy: () => void;
  /**
   * Rename this integration (its name is effectively its filename). Resolves true
   * on success and false when rejected (e.g. a duplicate name), so the inline
   * editor can stay open on conflict rather than silently reverting.
   */
  onRename: (name: string) => Promise<boolean>;
}


export default function IntegrationDetail({
  integration,
  folders,
  folderId,
  busy,
  onDelete,
  onCopy,
  onRename,
}: Props) {
  // Inline rename of the title. The parent keys this component by integration id,
  // so selecting another integration remounts it and resets the draft cleanly.
  // The integration's version tags, owned here so creating/deleting one in the
  // Versions section immediately updates the Deployments section's change-version
  // menu (the two sections render side by side).
  const [snapshots, setSnapshots] = useState<Snapshot[]>([]);
  const reloadSnapshots = useCallback(() => {
    listSnapshots(integration.id).then(setSnapshots, () => setSnapshots([]));
  }, [integration.id]);
  useEffect(() => {
    reloadSnapshots();
  }, [reloadSnapshots]);

  // The active version scoping the Resources (and Env) panels: a tag, or null for
  // the live working copy ("Current"). This component is keyed by integration id
  // upstream, so it resets to Current when another integration is selected.
  const [selectedTag, setSelectedTag] = useState<string | null>(null);
  // Resolve against the current tags so a deleted/absent tag transparently falls
  // back to Current — derived (not corrected via an effect) so the dropdown and
  // the scoped panels stay consistent without an extra render.
  const selectedSnapshot =
    (selectedTag && snapshots.find((s) => s.tag === selectedTag)) || null;
  const effectiveTag = selectedSnapshot ? selectedTag : null;

  // Tags currently deployed (by tag string, which is unique per integration),
  // derived from the live deployment list owned by the Deployments section. The
  // Versions section uses this to disable delete on a deployed tag.
  const [deployedTags, setDeployedTags] = useState<ReadonlySet<string>>(
    new Set(),
  );
  const onDeploymentsChange = useCallback((deployments: Deployment[]) => {
    setDeployedTags(
      new Set(
        deployments
          .map((d) => d.tag)
          .filter((t): t is string => Boolean(t)),
      ),
    );
  }, []);

  // The pod whose logs are docked at the bottom of the pane, or null when closed.
  // Lifted here (above the scroll area) so the panel docks under the whole detail
  // pane rather than inside the Deployments grid cell.
  const [logsPod, setLogsPod] = useState<{
    deploymentId: string;
    podName: string;
  } | null>(null);
  const openPodLogs = useCallback((deploymentId: string, podName: string) => {
    setLogsPod({ deploymentId, podName });
  }, []);

  // The Deploy button now lives in the header, but the deploy flow (modal, create,
  // env) stays owned by the Deployments section; the parent just controls the
  // modal's visibility.
  const [deployOpen, setDeployOpen] = useState(false);

  // The folder path ("Parent / Child"), or "No folder" when unfiled. Moving is done
  // by drag & drop in the tree, so this is read-only.
  const folderPath = useMemo(() => {
    if (!folderId) return "No folder";
    const byId = new Map(folders.map((f) => [f.id, f]));
    const parts: string[] = [];
    let cur: FlatFolder | undefined = byId.get(folderId);
    while (cur) {
      parts.unshift(cur.name);
      cur = cur.parentId ? byId.get(cur.parentId) : undefined;
    }
    return parts.join(" / ") || "No folder";
  }, [folders, folderId]);

  const updated = new Date(integration.lastUpdated);
  const updatedLabel = Number.isNaN(updated.getTime())
    ? integration.lastUpdated
    : updated.toLocaleString();

  // Prefer the actor's email, fall back to their name, then to an em dash when the
  // integration has no known creator/editor (local no-SSO, MCP writes, or a
  // since-removed user).
  const createdByLabel =
    integration.createdByEmail ?? integration.createdByName ?? "—";
  const updatedByLabel =
    integration.updatedByEmail ?? integration.updatedByName ?? "—";

  return (
    <div className="flex h-full flex-col">
      <IntegrationHeader
        integration={integration}
        snapshots={snapshots}
        deployedTags={deployedTags}
        busy={busy}
        effectiveTag={effectiveTag}
        onSelectTag={setSelectedTag}
        onRename={onRename}
        onDeploy={() => setDeployOpen(true)}
        onCopy={onCopy}
        onDelete={onDelete}
      />

      {/* Version pills: a status-at-a-glance row (green = deployed, grey = not) that
          doubles as a selector for the header dropdown. Only shown once tags exist. */}
      {snapshots.length > 0 && (
        <div className="px-4 pb-2">
          <VersionPills
            snapshots={snapshots}
            deployedTags={deployedTags}
            onSelectTag={setSelectedTag}
            onChanged={reloadSnapshots}
          />
        </div>
      )}

      {/* Two-column grid: Details (with the folded Definition stats) · Deployments /
          Resources · Env. Collapses to a single column on narrow widths. */}
      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
          <Section title="Details">
            <Row label="Folder" value={folderPath} />
            <Row label="Created by" value={createdByLabel} />
            <Row label="Last updated" value={updatedLabel} />
            <Row label="Updated by" value={updatedByLabel} />
            <Row
              label="ID"
              value={<span className="font-mono text-xs">{integration.id}</span>}
            />
            {/* Definition stats folded in at the bottom rather than a card of their
                own — scoped to the active version (a tag's frozen definition, or the
                working copy for Current). */}
            <div className="mt-3 border-t border-black/5 pt-3 dark:border-white/5">
              <h4 className="mb-2 text-[10px] font-semibold uppercase tracking-wide text-zinc-400">
                Definition
              </h4>
              <DefinitionSection
                key={selectedSnapshot?.id ?? integration.id}
                definition={selectedSnapshot?.definition ?? integration.definition}
              />
            </div>
          </Section>

          <Section title="Deployments">
            {/* Keyed by integration id so switching selection resets its state. */}
            <DeploymentsSection
              key={integration.id}
              integrationId={integration.id}
              integrationName={integration.name}
              snapshots={snapshots}
              activeSnapshot={selectedSnapshot}
              filterTag={effectiveTag}
              deployOpen={deployOpen}
              onDeployOpenChange={setDeployOpen}
              onDeploymentsChange={onDeploymentsChange}
              onSnapshotsChanged={reloadSnapshots}
              onOpenLogs={openPodLogs}
            />
          </Section>

          <Section title="Resources">
            {/* Scoped to the active version: the live working copy, or a tag's
                frozen (read-only) set. Keyed by integration id so it resets on
                selection; it reacts to version changes via props. */}
            <ResourcesSection
              key={integration.id}
              integrationId={integration.id}
              snapshotId={selectedSnapshot?.id}
              versionLabel={effectiveTag ?? undefined}
            />
          </Section>

          <Section title="Env">
            {/* Read-only declared env for the active version (values are set in
                the Deploy modal). Scoped to the same version as Resources. */}
            <EnvSection
              key={integration.id}
              integrationId={integration.id}
              snapshotId={selectedSnapshot?.id}
            />
          </Section>
        </div>
      </div>

      {/* Docked pod-log panel: tails one pod's logs at the bottom of the pane.
          Keyed by pod so switching pods resets the stream. */}
      {logsPod && (
        <PodLogPanel
          key={`${logsPod.deploymentId}:${logsPod.podName}`}
          deploymentId={logsPod.deploymentId}
          podName={logsPod.podName}
          onClose={() => setLogsPod(null)}
        />
      )}
    </div>
  );
}
