"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { ExternalLink, Trash2 } from "lucide-react";
import { fromDefinitionYaml } from "@octo/editor";
import {
  listSnapshots,
  type Integration,
  type Snapshot,
} from "@/app/model/orchestrator";
import DeploymentsSection from "./DeploymentsSection";
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
}

/** A labelled section wrapper; the unit future operating data plugs into. */
function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section className="border-t border-black/10 px-4 py-3 dark:border-white/10">
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
}: Props) {
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

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center gap-2 px-4 py-3">
        <h2 className="min-w-0 flex-1 truncate text-base font-semibold">
          {integration.name}
        </h2>
        <Link
          href={`/platform/i/${encodeURIComponent(integration.id)}`}
          className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500"
        >
          <ExternalLink size={14} />
          Open
        </Link>
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

      <div className="min-h-0 flex-1 overflow-y-auto">
        <Section title="Details">
          <Row label="Folder" value={folderPath} />
          <Row label="Last updated" value={updatedLabel} />
          <Row
            label="ID"
            value={<span className="font-mono text-xs">{integration.id}</span>}
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
            <p className="text-sm text-zinc-400">Definition could not be parsed.</p>
          )}
        </Section>

        <Section title="Versions">
          <SnapshotsSection
            integrationId={integration.id}
            snapshots={snapshots}
            onChanged={reloadSnapshots}
          />
        </Section>

        <Section title="Deployments">
          {/* Keyed by integration id so switching selection resets its state. */}
          <DeploymentsSection
            key={integration.id}
            integrationId={integration.id}
            integrationName={integration.name}
            snapshots={snapshots}
          />
        </Section>
      </div>
    </div>
  );
}
