"use client";

import { useState } from "react";
import { findBlock, findFlow } from "@/app/model/document";
import { findConnector } from "@/app/model/connectors";
import { useEditorState } from "@/app/state/editorState";
import BlockSettings from "./BlockSettings";
import SourceSettings from "./SourceSettings";
import FlowSettings from "./FlowSettings";
import ConnectionSettings from "./ConnectionSettings";

const MIN_WIDTH = 280;
const MAX_WIDTH = 560;
const DEFAULT_WIDTH = 340;

/**
 * Docked settings panel on the right edge. It shows the selected block's
 * settings, or — when no block is selected — the active flow's settings, both
 * driven by the editor document. The panel width is locally adjustable by
 * dragging its left divider (plain pointer events — kept out of the canvas
 * DndContext on purpose).
 */
export default function SettingsPanel() {
  const { state } = useEditorState();
  const [width, setWidth] = useState(DEFAULT_WIDTH);

  const connection = state.selectedConnectionId
    ? findConnector(state.document, state.selectedConnectionId)
    : undefined;
  const block =
    !connection && state.selectedBlockId
      ? findBlock(state.document, state.selectedBlockId)
      : undefined;
  const sourceFlow =
    !connection && !block && state.selectedSourceFlowId
      ? findFlow(state.document, state.selectedSourceFlowId)
      : undefined;
  const flow =
    !connection && !block && !sourceFlow?.source && state.activeFlowId
      ? findFlow(state.document, state.activeFlowId)
      : undefined;

  function startResize(e: React.PointerEvent) {
    e.preventDefault();
    const startX = e.clientX;
    const startWidth = width;
    const onMove = (ev: PointerEvent) => {
      // Dragging the left edge leftwards widens the panel.
      const next = startWidth + (startX - ev.clientX);
      setWidth(Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, next)));
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }

  return (
    <aside
      style={{ width }}
      className="relative shrink-0 border-l border-black/10 dark:border-white/10 flex flex-col"
    >
      {/* Resize divider */}
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize settings panel"
        onPointerDown={startResize}
        className="absolute inset-y-0 left-0 w-1.5 -translate-x-1/2 cursor-col-resize hover:bg-sky-400/40"
      />

      {connection ? (
        <ConnectionSettings connection={connection} />
      ) : block ? (
        <BlockSettings block={block} />
      ) : sourceFlow?.source ? (
        <SourceSettings flow={sourceFlow} />
      ) : flow ? (
        <FlowSettings flow={flow} />
      ) : (
        <div className="flex flex-1 items-center justify-center p-6 text-center text-sm text-zinc-400 dark:text-zinc-500">
          Select a component or flow to edit its settings.
        </div>
      )}
    </aside>
  );
}
