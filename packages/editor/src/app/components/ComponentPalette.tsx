"use client";

import { useCallback, useEffect, useState } from "react";
import { CommandPalette, paletteItemClasses } from "../../components/ui";
import { findFlow, owningFlowId } from "../model/document";
import { EditorActionType, useEditorState, type EditorState } from "../state/editorState";
import { palette } from "./palette";
import { rank, type RankedComponent } from "./paletteSearch";

/**
 * Add a component without reaching for the sidebar: Cmd/Ctrl+/ opens a search box
 * over the canvas, and Enter drops the first match into the flow you are working in.
 *
 * The list is `palette()`, the same capability-schema-derived list the sidebar shows,
 * so a block added to the runtime appears here with no change.
 */

/**
 * Where a new block lands: which flow, and where in it.
 *
 * Directly AFTER the selected block, which is what selecting something and then adding
 * means — you are building a chain from where you are, not appending to the bottom of
 * whatever flow that block happens to be in. Appending was the obvious first
 * implementation and the wrong one: with a block selected halfway down a flow it puts
 * the new one somewhere you are not looking.
 *
 * The flow is the selected block's own, because a block selected inside a composite
 * means that composite's branch is where you are working — `activeFlowId` only ever
 * names a top-level flow (it is set by clicking a flow card), so using it alone would
 * drop the block outside the branch on screen.
 *
 * With nothing selected there is no "after", so it appends to the active flow — and an
 * undefined flow falls through to ADD_BLOCK's own "no flow yet, make one" path.
 */
export function insertionPoint(state: EditorState): { flowId?: string; index?: number } {
  const selected = state.selectedBlockId;
  const flowId = selected ? owningFlowId(state.document, selected) : null;
  if (selected && flowId) {
    const at = findFlow(state.document, flowId)?.process.findIndex((b) => b.id === selected);
    // -1 cannot happen — owningFlowId just found the block in this flow's own chain —
    // but appending is the honest fallback if it ever did.
    if (at !== undefined && at >= 0) return { flowId, index: at + 1 };
  }
  return { flowId: flowId ?? state.activeFlowId ?? undefined };
}

export default function ComponentPalette() {
  const { state, dispatch } = useEditorState();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.key !== "/") return;
      // Unlike the other editor shortcuts this one is NOT typing-guarded on the way
      // in — being three fields deep in the settings panel is exactly when reaching
      // for the sidebar is most annoying. The guard is only for keys that mean a
      // character somewhere; "/" with a modifier never does.
      e.preventDefault();
      setQuery("");
      setActive(0);
      setOpen(true);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  const close = useCallback(() => setOpen(false), []);

  const items = open ? rank(palette(), query) : [];

  const pick = (item: RankedComponent) => {
    dispatch({
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: item.id, ...insertionPoint(state) },
    });
    // ADD_BLOCK selects what it added, so the next one lands after THAT — which is what
    // makes it possible to build a chain without touching the mouse.
    setOpen(false);
  };

  return (
    <CommandPalette
      open={open}
      onClose={close}
      query={query}
      onQueryChange={(q) => {
        setQuery(q);
        setActive(0);
      }}
      items={items}
      active={active}
      onActiveChange={setActive}
      onPick={pick}
      keyOf={(item) => item.id}
      label="Add a component"
      placeholder="Add a component…"
      empty={<p className="px-3 py-6 text-center text-sm text-zinc-500">No components match.</p>}
      renderItem={(item, isActive) => {
        const Icon = item.icon;
        return (
          <div className={paletteItemClasses(isActive)}>
            <Icon size={18} className="shrink-0 text-zinc-500" />
            <span>{item.label}</span>
            <span className="ml-auto text-xs text-zinc-400">{item.group}</span>
          </div>
        );
      }}
    />
  );
}
