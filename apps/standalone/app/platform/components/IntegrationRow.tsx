"use client";

import { useState } from "react";
import { ChevronRight, Download, FolderOpen, Tag } from "lucide-react";
import { paasBridge } from "../../desktop";
import type { IntegrationSummary, VersionSummary } from "../types";
import { useAction } from "../useAction";
import { MUTED, PRIMARY, SECONDARY, when } from "../ui";
import ErrorLine from "./ErrorLine";

/** One integration: open the folder that has it, or pull it into a new one, draft or version. */
export default function IntegrationRow({
  profile,
  integration,
  openPath,
}: {
  profile: string;
  integration: IntegrationSummary;
  openPath: string | null;
}) {
  const paas = paasBridge();
  const { busy, error, run } = useAction();
  const [versions, setVersions] = useState<VersionSummary[] | null>(null);
  const [expanded, setExpanded] = useState(false);
  const isOpen = integration.local !== undefined && integration.local === openPath;

  const toggle = () => {
    setExpanded((v) => !v);
    if (!versions && paas) void run(() => paas.versions(profile, integration.id).then(setVersions));
  };

  return (
    <li className="flex flex-col gap-2 px-4 py-3">
      <div className="flex items-center gap-3">
        <button type="button" onClick={toggle} aria-expanded={expanded} aria-label="Versions" className="text-zinc-400">
          <ChevronRight size={14} className={expanded ? "rotate-90 transition-transform" : "transition-transform"} />
        </button>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{integration.name}</div>
          <div className={MUTED}>
            Updated {when(integration.lastUpdated)}
            {integration.updatedByName ? ` by ${integration.updatedByName}` : ""}
            {integration.local ? ` · in ${integration.local}` : ""}
          </div>
        </div>
        {isOpen ? (
          <span className={MUTED}>Open now</span>
        ) : integration.local ? (
          <button
            type="button"
            disabled={busy}
            onClick={() => paas && void run(() => paas.openLocal(integration.local!))}
            className={SECONDARY}
          >
            <FolderOpen size={14} /> Open
          </button>
        ) : (
          <button
            type="button"
            disabled={busy}
            onClick={() => paas && void run(() => paas.pull(profile, integration.id, integration.name))}
            className={PRIMARY}
          >
            <Download size={14} /> Pull and open
          </button>
        )}
      </div>
      <ErrorLine error={error} />
      {expanded && (
        <ul className="ml-7 flex flex-col gap-1">
          {versions === null && <li className={MUTED}>Loading versions…</li>}
          {versions?.length === 0 && <li className={MUTED}>No versions yet</li>}
          {versions?.map((v) => (
            <li key={v.id} className="flex items-center gap-2 text-sm">
              <Tag size={13} className="text-zinc-400" />
              <span className="font-mono text-[13px]">{v.tag}</span>
              <span className={`${MUTED} flex-1`}>{when(v.createdAt)}</span>
              <button
                type="button"
                disabled={busy}
                onClick={() => paas && void run(() => paas.pull(profile, integration.id, integration.name, v.tag))}
                className={SECONDARY}
              >
                Open this version
              </button>
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}
