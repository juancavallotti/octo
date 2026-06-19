"use client";

import { useState } from "react";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import { PALETTE } from "./palette";
import PaletteBlock from "./PaletteBlock";

/**
 * Sidebar lists the integration building blocks (from the capability schema).
 * Clicking or dragging one adds a block to the active flow. The filter query is
 * small, local UI state (useState); adds are dispatched to the editor reducer.
 */
export default function Sidebar() {
  const { dispatch } = useEditorState();
  const [query, setQuery] = useState("");

  const items = PALETTE.filter((c) =>
    c.label.toLowerCase().includes(query.trim().toLowerCase()),
  );

  function addBlock(type: string) {
    dispatch({ type: EditorActionType.ADD_BLOCK, data: { blockType: type } });
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
      <ul className="px-2 pb-2 flex flex-col gap-1 overflow-y-auto">
        {items.map(({ id, label, icon }) => (
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
    </aside>
  );
}
