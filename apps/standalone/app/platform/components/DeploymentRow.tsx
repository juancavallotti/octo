"use client";

import { useState } from "react";
import { ExternalLink, Trash2 } from "lucide-react";
import { paasBridge } from "../../desktop";
import type { DeploymentSummary, VersionSummary } from "../types";
import { useAction } from "../useAction";
import { DANGER, INPUT, MUTED, SECONDARY } from "../ui";
import ErrorLine from "./ErrorLine";

const STATUS: Record<string, string> = {
  running: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
  failed: "bg-rose-500/15 text-rose-700 dark:text-rose-300",
};

/** One deployment: how it is doing, and rolling it to another version or scaling it. */
export default function DeploymentRow({
  deployment: d,
  versions,
  busy: running,
}: {
  deployment: DeploymentSummary;
  versions: VersionSummary[];
  busy: boolean;
}) {
  const paas = paasBridge();
  const { busy, error, run } = useAction();
  const [version, setVersion] = useState(d.tag);
  const [replicas, setReplicas] = useState(String(d.replicas));
  const disabled = busy || running || !paas;

  return (
    <li className="flex flex-col gap-2 px-4 py-3">
      <div className="flex items-center gap-3">
        <span className={`rounded px-1.5 py-0.5 text-[11px] font-medium ${STATUS[d.status] ?? "bg-black/[0.06] text-zinc-600 dark:bg-white/10 dark:text-zinc-300"}`}>
          {d.status}
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">
            {d.name} <span className="font-mono text-[13px] text-zinc-500">{d.tag}</span>
          </div>
          <div className={MUTED}>
            {d.readyReplicas}/{d.replicas} ready{d.reason ? ` · ${d.reason}` : ""}
          </div>
        </div>
        {d.externalUrl && (
          <a href={d.externalUrl} target="_blank" rel="noreferrer" className={SECONDARY}>
            <ExternalLink size={14} /> Open
          </a>
        )}
        <button
          type="button"
          disabled={disabled}
          onClick={() => paas && void run(() => paas.undeploy(d.id, d.name))}
          className={DANGER}
          aria-label={`Remove ${d.name}`}
        >
          <Trash2 size={14} />
        </button>
      </div>
      <div className="flex flex-wrap items-center gap-2 pl-1">
        <select value={version} onChange={(e) => setVersion(e.target.value)} aria-label="Version" className={INPUT}>
          {!versions.some((v) => v.tag === d.tag) && <option value={d.tag}>{d.tag}</option>}
          {versions.map((v) => (
            <option key={v.id} value={v.tag}>
              {v.tag}
            </option>
          ))}
        </select>
        <button
          type="button"
          disabled={disabled || version === d.tag}
          onClick={() => paas && void run(() => paas.rollout(d.id, { version }))}
          className={SECONDARY}
        >
          Roll out
        </button>
        <span className="mx-1 h-4 w-px bg-black/10 dark:bg-white/10" />
        <input type="number" min={0} max={100} value={replicas} onChange={(e) => setReplicas(e.target.value)} aria-label="Replicas" className={`${INPUT} w-16`} />
        <button
          type="button"
          disabled={disabled || Number(replicas) === d.replicas}
          onClick={() => paas && void run(() => paas.scale(d.id, Number(replicas)))}
          className={SECONDARY}
        >
          Scale
        </button>
      </div>
      <ErrorLine error={error} />
    </li>
  );
}
