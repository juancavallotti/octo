"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type RefObject,
} from "react";
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
import { folderPathOf, type FlatFolder } from "./managerViews";
import IntegrationHeader from "./IntegrationHeader";

/**
 * Read-only operating details for the selected integration, plus its primary
 * actions (open in the editor, move to a folder, delete), laid out as a list of
 * labelled sections.
 */

interface Props {
  integration: Integration;
  /** Flattened folders, used to render the current folder's path. */
  folders: FlatFolder[];
  /** The folder the integration currently belongs to, or null when unfiled. */
  folderId: string | null;
  busy: boolean;
  /** Hidden file input backing the header's "Replace from bundle". */
  replaceInput: RefObject<HTMLInputElement | null>;
  /** Download the active version — a tag's frozen contents, or the working copy —
   * as a bundle archive. */
  onDownloadBundle: (snapshot: { id: string; tag: string } | null) => void;
  /** Overwrite this integration's definition and resources from a bundle. */
  onReplaceFromBundle: (file: File) => void;
  onDelete: () => void;
  /** Duplicate this integration into a new "Copy of …" record. */
  onCopy: () => void;
  /**
   * Rename this integration (its name is effectively its filename). Resolves true
   * on success and false when rejected (e.g. a duplicate name), so the inline
   * editor can stay open on conflict rather than silently reverting.
   */
  onRename: (name: string) => Promise<boolean>;
  /** Choose the integration's icon; "" hands it back to the derivation. */
  onSelectIcon: (icon: string) => void;
}

export default function IntegrationDetail({
  integration,
  folders,
  folderId,
  busy,
  replaceInput,
  onDownloadBundle,
  onReplaceFromBundle,
  onDelete,
  onCopy,
  onRename,
  onSelectIcon,
}: Props) {
  // The integration's version tags, owned here so creating or deleting one in the
  // Versions section immediately updates the Deployments section's change-version
  // menu.
  const [snapshots, setSnapshots] = useState<Snapshot[]>([]);
  const reloadSnapshots = useCallback(() => {
    listSnapshots(integration.id).then(setSnapshots, () => setSnapshots([]));
  }, [integration.id]);
  useEffect(() => {
    reloadSnapshots();
  }, [reloadSnapshots]);

  // The active version scoping the Resources (and Env) panels: a tag, or null for
  // the live working copy ("Current").
  const [selectedTag, setSelectedTag] = useState<string | null>(null);
  // Resolved against the current tags so a deleted or absent tag falls back to
  // Current — derived rather than corrected in an effect, so the dropdown and the
  // scoped panels stay consistent without an extra render.
  const selectedSnapshot =
    (selectedTag && snapshots.find((s) => s.tag === selectedTag)) || null;
  const effectiveTag = selectedSnapshot ? selectedTag : null;

  // Tags currently deployed, by tag string — unique per integration. What keeps
  // the Versions section from offering to delete a tag something is running.
  const [deployedTags, setDeployedTags] = useState<ReadonlySet<string>>(
    new Set(),
  );
  const onDeploymentsChange = useCallback((deployments: Deployment[]) => {
    setDeployedTags(
      new Set(
        deployments.map((d) => d.tag).filter((t): t is string => Boolean(t)),
      ),
    );
  }, []);

  // The pod whose logs are docked at the bottom of the pane, or null when closed.
  // Lifted above the scroll area so the panel docks under the whole detail pane
  // rather than inside the Deployments grid cell.
  const [logsPod, setLogsPod] = useState<{
    deploymentId: string;
    podName: string;
  } | null>(null);
  const openPodLogs = useCallback((deploymentId: string, podName: string) => {
    setLogsPod({ deploymentId, podName });
  }, []);

  // The deploy flow (modal, create, env) belongs to the Deployments section; only
  // the modal's visibility is owned here, because the button is in the header.
  const [deployOpen, setDeployOpen] = useState(false);

  // Read-only: moving is done by drag and drop in the tree.
  const folderPath = useMemo(
    () => folderPathOf(folders, folderId),
    [folders, folderId],
  );

  const updated = new Date(integration.lastUpdated);
  const updatedLabel = Number.isNaN(updated.getTime())
    ? integration.lastUpdated
    : updated.toLocaleString();

  // An em dash when there is no known creator or editor — a row that predates
  // attribution, or a user who has since been removed.
  const createdByLabel =
    integration.createdByEmail ?? integration.createdByName ?? "—";
  const updatedByLabel =
    integration.updatedByEmail ?? integration.updatedByName ?? "—";

  return (
    <div className="flex h-full flex-col">
      <IntegrationHeader
        integration={integration}
        snapshots={snapshots}
        activeSnapshot={selectedSnapshot}
        deployedTags={deployedTags}
        busy={busy}
        effectiveTag={effectiveTag}
        replaceInput={replaceInput}
        onSelectTag={setSelectedTag}
        onRename={onRename}
        onSelectIcon={onSelectIcon}
        onDownloadBundle={onDownloadBundle}
        onReplaceFromBundle={onReplaceFromBundle}
        onDeploy={() => setDeployOpen(true)}
        onCopy={onCopy}
        onDelete={onDelete}
      />

      {/* Status at a glance — green deployed, grey not — doubling as a selector
          for the header dropdown. Only shown once tags exist. */}
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

      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
          <Section title="Details">
            <Row label="Folder" value={folderPath} />
            <Row label="Created by" value={createdByLabel} />
            <Row label="Last updated" value={updatedLabel} />
            <Row label="Updated by" value={updatedByLabel} />
            <Row
              label="ID"
              value={
                <span className="font-mono text-xs">{integration.id}</span>
              }
            />
            {/* Scoped to the active version: a tag's frozen definition, or the
                working copy for Current. */}
            <div className="mt-3 border-t border-black/5 pt-3 dark:border-white/5">
              <h4 className="mb-2 text-[10px] font-semibold uppercase tracking-wide text-zinc-400">
                Definition
              </h4>
              <DefinitionSection
                key={selectedSnapshot?.id ?? integration.id}
                definition={
                  selectedSnapshot?.definition ?? integration.definition
                }
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
                frozen (read-only) set. */}
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

      {/* Tails one pod's logs at the bottom of the pane. Keyed by pod so
          switching pods resets the stream. */}
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
