"use client";

import { useEffect, useRef, useState } from "react";
import { ChevronDown, ChevronUp, Square } from "lucide-react";
import { TerminalText } from "@octo/editor";
import { paasBridge } from "../../desktop";
import type { Activity } from "../types";

/** Enough to scroll back through a deploy, not so much that a long follow grows without end. */
const KEEP = 500;

/**
 * What the running operation is saying, the way the editor's console shows a run:
 * a bar naming the last operation and how it went, opening onto its output.
 */
export default function OutputDrawer({ activity }: { activity: Activity | null }) {
  const [lines, setLines] = useState<string[]>([]);
  // Open while an operation runs or after one failed, unless the user said otherwise
  // about this one.
  const [toggled, setToggled] = useState<{ at: number | undefined; open: boolean } | null>(null);
  const open =
    toggled && toggled.at === activity?.at ? toggled.open : activity !== null && activity.state !== "done";
  const end = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const paas = paasBridge();
    if (!paas) return;
    return paas.onOutput(({ line }) => setLines((all) => [...all, line].slice(-KEEP)));
  }, []);

  useEffect(() => {
    if (open) end.current?.scrollIntoView({ block: "end" });
  }, [lines, open]);

  if (!activity && lines.length === 0) return null;
  const tone =
    activity?.state === "failed"
      ? "text-rose-600 dark:text-rose-400"
      : activity?.state === "running"
        ? "text-sky-600 dark:text-sky-400"
        : "text-zinc-500";

  return (
    <section className="shrink-0 border-t border-black/10 dark:border-white/10">
      <div className="flex h-9 items-center gap-2 px-4 text-[13px]">
        <button type="button" onClick={() => setToggled({ at: activity?.at, open: !open })} className="flex min-w-0 flex-1 items-center gap-2 text-left">
          {open ? <ChevronDown size={14} /> : <ChevronUp size={14} />}
          <span className={`truncate ${tone}`}>
            {activity ? `${activity.label}${activity.state === "running" ? "…" : activity.state === "failed" ? " — failed" : " — done"}` : "Output"}
          </span>
        </button>
        {activity?.state === "running" && (
          <button
            type="button"
            onClick={() => void paasBridge()?.cancel()}
            className="flex items-center gap-1 rounded px-2 py-0.5 text-zinc-600 hover:bg-black/[0.05] dark:text-zinc-300 dark:hover:bg-white/[0.08]"
          >
            <Square size={12} /> Cancel
          </button>
        )}
      </div>
      {open && (
        <div className="max-h-56 overflow-y-auto bg-zinc-50 px-4 py-2 font-mono text-xs leading-5 dark:bg-zinc-950">
          {lines.map((l, i) => (
            <div key={i} className="whitespace-pre-wrap break-all">
              <TerminalText text={l} />
            </div>
          ))}
          <div ref={end} />
        </div>
      )}
    </section>
  );
}
