"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { useEditorState, EditorActionType } from "../state/editorState";
import { palette, paletteGroups } from "./palette";
import PaletteBlock from "./PaletteBlock";

/**
 * Sidebar lists the integration building blocks (from the capability schema),
 * grouped into collapsible sections by their schema `group`. Clicking or dragging
 * one adds a block to the active flow. The filter query and the set of collapsed
 * groups are small, local UI state (useState); adds are dispatched to the editor
 * reducer. While a filter is active every group is force-expanded so matches are
 * never hidden behind a collapsed header.
 */
export default function Sidebar() {
  const { dispatch } = useEditorState();
  const [query, setQuery] = useState("");
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());

  const filter = query.trim().toLowerCase();
  const filtering = filter.length > 0;
  const items = palette().filter((c) =>
    c.label.toLowerCase().includes(filter),
  );

  function addBlock(type: string) {
    dispatch({ type: EditorActionType.ADD_BLOCK, data: { blockType: type } });
  }

  function toggle(group: string) {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(group)) next.delete(group);
      else next.add(group);
      return next;
    });
  }

  return (
    <aside className="w-60 shrink-0 border-r border-black/10 dark:border-white/10 flex flex-col">
      <h2 className="px-4 pt-3 text-xs font-semibold uppercase tracking-wide text-zinc-500">
        Components
      </h2>
      <div className="px-3 py-2">
        <input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filter…"
          aria-label="Filter components"
          className="w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30"
        />
      </div>
      <div className="flex flex-col overflow-y-auto pb-2">
        {paletteGroups().map((group) => {
          const groupItems = items.filter((c) => c.group === group);
          if (groupItems.length === 0) return null;
          const open = filtering || !collapsed.has(group);
          return (
            <section key={group}>
              <button
                type="button"
                onClick={() => toggle(group)}
                aria-expanded={open}
                className="flex w-full items-center gap-1 px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
              >
                {open ? (
                  <ChevronDown size={14} className="shrink-0" />
                ) : (
                  <ChevronRight size={14} className="shrink-0" />
                )}
                <span className="truncate">{group}</span>
                <span className="ml-auto text-[10px] font-medium text-zinc-400 dark:text-zinc-500">
                  {groupItems.length}
                </span>
              </button>
              {open && (
                <ul className="px-2 pb-1 flex flex-col gap-1">
                  {groupItems.map(({ id, label, icon }) => (
                    <li key={id}>
                      <PaletteBlock
                        type={id}
                        label={label}
                        icon={icon}
                        onAdd={() => addBlock(id)}
                      />
                    </li>
                  ))}
                </ul>
              )}
            </section>
          );
        })}
      </div>
    </aside>
  );
}
