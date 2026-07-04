"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { Copy, Download, Pencil, Trash2 } from "lucide-react";
import { fromDefinitionYaml } from "@octo/editor";
import { downloadDefinition } from "./yamlFile";
import {
  listSnapshots,
  type Deployment,
  type Integration,
  type Snapshot,
} from "@/app/model/orchestrator";
import DeploymentsSection from "./DeploymentsSection";
import EnvSection from "./EnvSection";
import PodLogPanel from "./PodLogPanel";
import ResourcesSection from "./ResourcesSection";
import SnapshotsSection from "./SnapshotsSection";

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

/** A labelled section card; the unit the detail grid is composed of. `className`
 * lets a cell span columns in the grid. */
function Section({
  title,
  className,
  children,
}: {
  title: string;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <section
      className={`rounded-lg border border-black/10 p-3 dark:border-white/10 ${className ?? ""}`}
    >
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-zinc-400">
        {title}
      </h3>
      {children}
    </section>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-0.5 text-sm">
      <span className="text-zinc-500">{label}</span>
      <span className="min-w-0 truncate text-right font-medium">{value}</span>
    </div>
  );
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
  const [editingName, setEditingName] = useState(false);
  const [nameDraft, setNameDraft] = useState(integration.name);
  const cancelRename = useRef(false);
  const nameInput = useRef<HTMLInputElement>(null);
  const commitRename = async () => {
    if (cancelRename.current) {
      cancelRename.current = false;
      setEditingName(false);
      return;
    }
    const name = nameDraft.trim();
    if (!name || name === integration.name) {
      setEditingName(false);
      return;
    }
    // Keep the editor open until the rename is accepted; a rejected name (e.g. a
    // duplicate) leaves the field open and re-focused so it can be corrected. The
    // error itself is surfaced by the parent's inline banner.
    const ok = await onRename(name);
    if (ok) setEditingName(false);
    else nameInput.current?.focus();
  };
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
  // Summarize the stored definition; tolerate definitions we can't parse.
  const summary = useMemo(() => {
    try {
      const doc = fromDefinitionYaml(integration.definition);
      const sources = doc.flows.filter((f) => f.source).length;
      return {
        flows: doc.flows.length,
        connectors: doc.connectors.length,
        sources,
      };
    } catch {
      return null;
    }
  }, [integration.definition]);

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
      <header className="flex items-center gap-2 px-4 py-3">
        {editingName ? (
          <input
            ref={nameInput}
            autoFocus
            value={nameDraft}
            disabled={busy}
            aria-label="Integration name"
            onChange={(e) => setNameDraft(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === "Enter") e.currentTarget.blur();
              else if (e.key === "Escape") {
                cancelRename.current = true;
                e.currentTarget.blur();
              }
            }}
            className="min-w-0 flex-1 rounded-md border border-black/10 bg-transparent px-1.5 py-0.5 text-base font-semibold outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
          />
        ) : (
          <button
            type="button"
            onClick={() => {
              setNameDraft(integration.name);
              setEditingName(true);
            }}
            title="Rename integration"
            className="min-w-0 flex-1 truncate text-left text-base font-semibold hover:underline"
          >
            {integration.name}
          </button>
        )}
        <Link
          href={`/platform/i/${encodeURIComponent(integration.id)}`}
          className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500"
        >
          <Pencil size={14} />
          Edit
        </Link>
        <button
          type="button"
          onClick={onCopy}
          disabled={busy}
          aria-label="Duplicate integration"
          title="Duplicate integration"
          className="rounded-md p-1.5 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 disabled:opacity-50 dark:hover:bg-white/10 dark:hover:text-zinc-200"
        >
          <Copy size={16} />
        </button>
        <button
          type="button"
          onClick={() =>
            downloadDefinition(integration.name, integration.definition)
          }
          aria-label="Download integration YAML"
          title="Download YAML"
          className="rounded-md p-1.5 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 dark:hover:bg-white/10 dark:hover:text-zinc-200"
        >
          <Download size={16} />
        </button>
        <button
          type="button"
          onClick={onDelete}
          disabled={busy}
          aria-label="Delete integration"
          className="rounded-md p-1.5 text-zinc-400 transition-colors hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
        >
          <Trash2 size={16} />
        </button>
      </header>

      {/* Two-column grid: Details · Deployments / Versions · Definition /
          Resources (full width until the Env panel joins it). Collapses to a
          single column on narrow widths. */}
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
          </Section>

          <Section title="Deployments">
            {/* Keyed by integration id so switching selection resets its state. */}
            <DeploymentsSection
              key={integration.id}
              integrationId={integration.id}
              integrationName={integration.name}
              snapshots={snapshots}
              onDeploymentsChange={onDeploymentsChange}
              onOpenLogs={openPodLogs}
            />
          </Section>

          <Section title="Versions">
            <SnapshotsSection
              integrationId={integration.id}
              snapshots={snapshots}
              deployedTags={deployedTags}
              selectedTag={effectiveTag}
              onSelectTag={setSelectedTag}
              onChanged={reloadSnapshots}
            />
          </Section>

          <Section title="Definition">
            {summary ? (
              <>
                <Row label="Flows" value={summary.flows} />
                <Row label="Sources" value={summary.sources} />
                <Row label="Connectors" value={summary.connectors} />
              </>
            ) : (
              <p className="text-sm text-zinc-400">
                Definition could not be parsed.
              </p>
            )}
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
