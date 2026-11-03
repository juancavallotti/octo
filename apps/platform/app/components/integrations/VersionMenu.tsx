"use client";

import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, GitBranch, Tag } from "lucide-react";
import type { Snapshot } from "@/app/model/orchestrator";

/**
 * A GitHub-style version picker: a button showing the active version that opens a
 * popover menu of the working copy plus every tag, with a check on the selected
 * one and a green dot on the tags in `deployedTags`. Closes on outside click or
 * Escape.
 */
export default function VersionMenu({
  snapshots,
  deployedTags,
  value,
  onChange,
  align = "end",
}: {
  snapshots: Snapshot[];
  /** The tags marked with a green dot: the ones the caller calls deployed. */
  deployedTags: ReadonlySet<string>;
  /** The active tag, or null for Current (the working copy). */
  value: string | null;
  onChange: (tag: string | null) => void;
  /** Which edge of the button the menu lines up with: "end" opens leftward, for
   * a button near the right of the page; "start" opens rightward. */
  align?: "start" | "end";
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const pick = (tag: string | null) => {
    onChange(tag);
    setOpen(false);
  };

  const label = value ?? "Working copy";

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
        title="Select version"
        className="inline-flex items-center gap-1.5 rounded-md border border-black/10 bg-transparent px-2.5 py-1 text-sm font-medium transition-colors hover:bg-black/[0.04] dark:border-white/15 dark:hover:bg-white/[0.06]"
      >
        <GitBranch size={14} className="text-zinc-400" />
        <span className="max-w-[10rem] truncate">{label}</span>
        <ChevronDown size={14} className="text-zinc-400" />
      </button>

      {open && (
        <div
          role="menu"
          className={`absolute ${align === "start" ? "left-0" : "right-0"} top-full z-30 mt-1 max-h-72 w-56 overflow-y-auto rounded-lg border border-black/10 bg-white py-1 shadow-lg dark:border-white/10 dark:bg-zinc-900`}
        >
          <button
            type="button"
            role="menuitemradio"
            aria-checked={value === null}
            onClick={() => pick(null)}
            className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-sm hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
          >
            <Check
              size={14}
              className={value === null ? "text-sky-500" : "invisible"}
            />
            <GitBranch size={12} className="shrink-0 text-zinc-400" />
            <span className="flex-1 truncate">Working copy</span>
            <Dot deployed={false} />
          </button>

          {snapshots.length > 0 && (
            <div className="my-1 border-t border-black/5 dark:border-white/5" />
          )}

          {snapshots.map((s) => {
            const deployed = deployedTags.has(s.tag);
            const active = value === s.tag;
            return (
              <button
                key={s.id}
                type="button"
                role="menuitemradio"
                aria-checked={active}
                onClick={() => pick(s.tag)}
                className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-sm hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
              >
                <Check
                  size={14}
                  className={active ? "text-sky-500" : "invisible"}
                />
                <Tag size={12} className="shrink-0 text-zinc-400" />
                <span className="flex-1 truncate">{s.tag}</span>
                <Dot deployed={deployed} />
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

/** A version's mark: green when it is deployed, gray otherwise. */
function Dot({ deployed }: { deployed: boolean }) {
  return (
    <span
      aria-label={deployed ? "deployed" : undefined}
      title={deployed ? "Deployed" : undefined}
      className={`h-2 w-2 shrink-0 rounded-full ${
        deployed ? "bg-emerald-500" : "bg-zinc-300 dark:bg-zinc-600"
      }`}
    />
  );
}
