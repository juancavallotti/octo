"use client";

import { useEffect, useState } from "react";
import { FolderOpen, Tag, Trash2 } from "lucide-react";
import { paasBridge } from "../../desktop";
import type { VersionSummary } from "../types";
import { useAction } from "../useAction";
import { nextTag } from "../versions";
import { CARD, DANGER, INPUT, MUTED, PRIMARY, SECONDARY, when } from "../ui";
import ErrorLine from "./ErrorLine";

/** The open folder's integration's versions, and freezing a new one. */
export default function VersionsSection({ busy: running }: { busy: boolean }) {
  const paas = paasBridge();
  const { busy, error, run } = useAction();
  const [versions, setVersions] = useState<VersionSummary[] | null>(null);
  const [tag, setTag] = useState<string | null>(null);
  const [pushFirst, setPushFirst] = useState(true);

  const reload = () => paas && void run(() => paas.linkedVersions().then(setVersions));
  useEffect(() => {
    reload();
    // Once per integration; the parent keys this section on it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const suggested = versions ? nextTag(versions.map((v) => v.tag)) : "";
  const value = tag ?? suggested;
  const act = (fn: () => Promise<unknown>) =>
    void run(async () => {
      await fn();
      setTag(null);
      if (paas) setVersions(await paas.linkedVersions());
    });

  return (
    <div className="flex flex-col gap-2">
      <div className={`${CARD} flex flex-wrap items-center gap-2 p-3`}>
        <Tag size={14} className="text-zinc-400" />
        <input value={value} onChange={(e) => setTag(e.target.value)} aria-label="New version's tag" placeholder="v1.0.0" className={`${INPUT} w-32 font-mono`} />
        <label className="flex items-center gap-1.5 text-sm">
          <input type="checkbox" checked={pushFirst} onChange={(e) => setPushFirst(e.target.checked)} />
          Push this folder first
        </label>
        <span className="flex-1" />
        <button
          type="button"
          disabled={busy || running || !value || !paas}
          onClick={() => paas && act(() => paas.createVersion(value, pushFirst))}
          className={PRIMARY}
        >
          Create version
        </button>
      </div>
      <ErrorLine error={error} />
      {versions === null && <p className={MUTED}>Loading…</p>}
      {versions?.length === 0 && <p className={MUTED}>No versions yet. A version freezes the draft, to deploy it.</p>}
      {versions && versions.length > 0 && (
        <ul className={`${CARD} divide-y divide-black/5 dark:divide-white/5`}>
          {[...versions].sort((a, b) => b.createdAt.localeCompare(a.createdAt)).map((v) => (
            <li key={v.id} className="flex items-center gap-3 px-4 py-2">
              <span className="font-mono text-[13px]">{v.tag}</span>
              <span className={`${MUTED} flex-1`}>{when(v.createdAt)}</span>
              <button type="button" disabled={busy} onClick={() => paas && void run(() => paas.pullVersion(v.tag))} className={SECONDARY}>
                <FolderOpen size={14} /> Open
              </button>
              <button type="button" disabled={busy || running} onClick={() => paas && act(() => paas.removeVersion(v.tag))} className={DANGER} aria-label={`Delete ${v.tag}`}>
                <Trash2 size={14} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
