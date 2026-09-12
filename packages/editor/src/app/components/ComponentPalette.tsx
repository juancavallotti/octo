"use client";

import { useCallback, useEffect, useState } from "react";
import { CommandPalette, paletteItemClasses } from "../../components/ui";
import { owningFlowId } from "../model/document";
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
 * Which flow a new block lands in.
 *
 * The selected block's own flow first, because a block selected inside a composite
 * means that composite's branch is where you are working — `activeFlowId` only ever
 * names a top-level flow (it is set by clicking a flow card), so using it alone would
 * drop the block outside the branch the user was looking at.
 *
 * Null falls through to ADD_BLOCK's own "no flow yet, make one" path.
 */
export function targetFlowId(state: EditorState): string | undefined {
  const owner = state.selectedBlockId
    ? owningFlowId(state.document, state.selectedBlockId)
    : null;
  return owner ?? state.activeFlowId ?? undefined;
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
      data: { blockType: item.id, flowId: targetFlowId(state) },
    });
    // ADD_BLOCK selects what it added, so the next one lands after it — which is what
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
