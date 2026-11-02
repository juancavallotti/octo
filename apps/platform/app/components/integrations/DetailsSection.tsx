"use client";

import { useMemo } from "react";
import type { Integration } from "@/app/model/orchestrator";
import { SkeletonLine } from "@/app/components/ui/Skeleton";
import DefinitionSection from "./DefinitionSection";
import { Row, Section } from "./DetailLayout";
import { folderPathOf, type FlatFolder } from "./managerViews";

/**
 * The Details card: who made the integration and when, where it is filed, and the
 * at-a-glance numbers of the version in view.
 */
export default function DetailsSection({
  integration,
  folders,
  folderId,
  versionKey,
  versionLabel,
  definition,
}: {
  integration: Integration;
  folders: FlatFolder[];
  folderId: string | null;
  /** Identifies the version in view, so its numbers reset when it changes. */
  versionKey: string;
  /** The tag in view, or null for the working copy. */
  versionLabel: string | null;
  /** That version's definition; null while a tag's is still being read. */
  definition: string | null;
}) {
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
    <Section title="Details">
      <Row label="Folder" value={folderPath} />
      <Row label="Created by" value={createdByLabel} />
      <Row label="Last updated" value={updatedLabel} />
      <Row label="Updated by" value={updatedByLabel} />
      <Row
        label="ID"
        value={<span className="font-mono text-xs">{integration.id}</span>}
      />
      <div className="mt-3 border-t border-black/5 pt-3 dark:border-white/5">
        <h4 className="mb-2 text-[10px] font-semibold uppercase tracking-wide text-zinc-400">
          Definition
        </h4>
        {definition === null ? (
          <div aria-busy aria-label={`Reading ${versionLabel ?? "the definition"}`}>
            <SkeletonLine width="70%" className="h-5" />
          </div>
        ) : (
          <DefinitionSection key={versionKey} definition={definition} />
        )}
      </div>
    </Section>
  );
}
