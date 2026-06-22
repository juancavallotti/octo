"use client";

import { createElement, useEffect, useRef, useState } from "react";
import { Cable, Plus, X } from "lucide-react";
import { getConnectorSpec, listConnectors, resolveIcon } from "@/app/schema";
import { useEditorState, EditorActionType } from "@/app/state/editorState";

const CONNECTORS = listConnectors();

/**
 * Floating launcher pinned to the top-left of the canvas. The button opens a
 * popover listing the document's connections (connector instances); clicking one
 * selects it so its settings open in the right panel. An "Add connection" row
 * expands a menu of connector types — picking one creates and selects a new
 * connection, mirroring how SourcePicker adds a source.
 */
export default function ConnectionsLauncher() {
  const { state, dispatch } = useEditorState();
  const [open, setOpen] = useState(false);
  const [adding, setAdding] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  const connections = state.document.connectors;

  // Closing always collapses the add-submenu so it reopens to the list.
  const close = () => {
    setOpen(false);
    setAdding(false);
  };

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) close();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
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
        aria-label="Connections"
        onClick={() => (open ? close() : setOpen(true))}
        className="flex items-center gap-1.5 rounded-full border border-black/10 bg-white/90 px-3 py-1.5 text-sm text-zinc-600 shadow-sm backdrop-blur transition-colors hover:bg-white hover:text-zinc-900 dark:border-white/15 dark:bg-zinc-900/90 dark:text-zinc-300 dark:hover:bg-zinc-900 dark:hover:text-zinc-100"
      >
        <Cable size={16} />
        Connections
        {connections.length > 0 && (
          <span className="rounded-full bg-black/[0.06] px-1.5 text-xs tabular-nums dark:bg-white/10">
            {connections.length}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute left-0 top-full z-20 mt-2 w-64 overflow-hidden rounded-xl border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900">
          {connections.length === 0 ? (
            <p className="px-3 py-3 text-sm text-zinc-400 dark:text-zinc-500">
              No connections yet.
            </p>
          ) : (
            <ul className="max-h-72 overflow-y-auto py-1">
              {connections.map((c) => {
                const spec = getConnectorSpec(c.type);
                return (
                  <li key={c.id} className="group/row relative">
                    <button
                      type="button"
                      onClick={() => {
                        dispatch({
                          type: EditorActionType.SELECT_CONNECTION,
                          data: { id: c.id },
                        });
                        close();
                      }}
                      className={`flex w-full items-center gap-2.5 px-3 py-2 pr-8 text-left transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06] ${
                        state.selectedConnectionId === c.id
                          ? "bg-sky-500/10"
                          : ""
                      }`}
                    >
                      {createElement(resolveIcon(spec?.icon ?? ""), {
                        size: 18,
                        className: "text-zinc-500 shrink-0",
                      })}
                      <span className="flex min-w-0 flex-col leading-tight">
                        <span className="truncate text-sm font-medium">
                          {c.name}
                        </span>
                        <span className="text-xs text-zinc-400 dark:text-zinc-500">
                          {spec?.label ?? c.type}
                        </span>
                      </span>
                    </button>
                    <button
                      type="button"
                      aria-label={`Remove ${c.name}`}
                      onClick={() =>
                        dispatch({
                          type: EditorActionType.REMOVE_CONNECTION,
                          data: { id: c.id },
                        })
                      }
                      className="absolute right-2 top-1/2 -translate-y-1/2 rounded-full p-1 text-zinc-400 opacity-0 transition-opacity hover:text-red-500 group-hover/row:opacity-100"
                    >
                      <X size={14} />
                    </button>
                  </li>
                );
              })}
            </ul>
          )}

          <div className="border-t border-black/10 dark:border-white/10">
            {adding ? (
              <ul className="max-h-60 overflow-y-auto py-1">
                {CONNECTORS.map((spec) => (
                  <button
                    key={spec.type}
                    type="button"
                    onClick={() => {
                      close();
                      dispatch({
                        type: EditorActionType.ADD_CONNECTION,
                        data: { type: spec.type },
                      });
                    }}
                    className="flex w-full items-center gap-2.5 px-3 py-2 text-left transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
                  >
                    {createElement(resolveIcon(spec.icon ?? ""), {
                      size: 18,
                      className: "text-zinc-500 shrink-0",
                    })}
                    <span className="text-sm font-medium">{spec.label}</span>
                  </button>
                ))}
              </ul>
            ) : (
              <button
                type="button"
                onClick={() => setAdding(true)}
                className="flex w-full items-center gap-1.5 px-3 py-2 text-sm text-zinc-500 transition-colors hover:bg-black/[0.04] hover:text-zinc-700 dark:hover:bg-white/[0.06] dark:hover:text-zinc-300"
              >
                <Plus size={14} />
                Add connection
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
