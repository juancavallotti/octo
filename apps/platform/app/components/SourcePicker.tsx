"use client";

import { createElement, useEffect, useRef, useState } from "react";
import { Plus } from "lucide-react";
import { listSources, resolveIcon } from "@/app/schema";
import { useEditorState, EditorActionType } from "@/app/state/editorState";

const SOURCES = listSources();

/**
 * The "Add source" control for a flow with no source yet: a dashed button that
 * opens a dropdown of every available source type. Picking one attaches it to the
 * flow (and the reducer selects it, so the settings panel opens). Clicks are kept
 * from bubbling to FlowCard's SET_ACTIVE_FLOW.
 */
export default function SourcePicker({ flowId }: { flowId: string }) {
  const { dispatch } = useEditorState();
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

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          setOpen((v) => !v);
        }}
        className="flex items-center gap-1.5 rounded-full border border-dashed border-zinc-300 px-3 py-1.5 text-sm text-zinc-500 transition-colors hover:border-zinc-400 hover:text-zinc-700 dark:border-zinc-700 dark:hover:border-zinc-500 dark:hover:text-zinc-300"
      >
        <Plus size={14} />
        Add source
      </button>

      {open && (
        <div
          onClick={(e) => e.stopPropagation()}
          className="absolute left-1/2 top-full z-20 mt-2 w-60 -translate-x-1/2 overflow-hidden rounded-xl border border-black/10 bg-white py-1 shadow-lg dark:border-white/10 dark:bg-zinc-900"
        >
          {SOURCES.map(({ connector, connectorLabel, spec }) => (
            <button
              key={`${connector}:${spec.type}`}
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                setOpen(false);
                dispatch({
                  type: EditorActionType.ADD_SOURCE,
                  data: { flowId, connector, type: spec.type },
                });
              }}
              className="flex w-full items-center gap-2.5 px-3 py-2 text-left transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
            >
              {createElement(resolveIcon(spec.icon ?? ""), {
                size: 18,
                className: "text-zinc-500 shrink-0",
              })}
              <span className="flex flex-col leading-tight">
                <span className="text-sm font-medium">{spec.label}</span>
                <span className="text-xs text-zinc-400 dark:text-zinc-500">
                  {connectorLabel}
                </span>
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
