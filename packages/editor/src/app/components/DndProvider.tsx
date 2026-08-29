"use client";

import { ReactNode, useState } from "react";
import {
  closestCenter,
  pointerWithin,
  DndContext,
  DragEndEvent,
  DragOverlay,
  DragStartEvent,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { findBlock, findFlow } from "../model/document";
import { useEditorState, EditorActionType } from "../state/editorState";
import { DragData, DropData } from "./dnd";
import DragPreview from "./DragPreview";
import { useCanvasZoom } from "../canvas/ZoomContext";

/**
 * A single DndContext spanning the editor body so the palette (drag sources) and
 * every flow's blocks (drag sources) share one drag session with the insertion
 * gaps (drop targets). onDragEnd is the one place a drop becomes a reducer
 * action: a palette drag inserts a new block at the gap's index; a canvas drag
 * reorders within its flow or moves across flows (including into nested slots).
 */
export default function DndProvider({ children }: { children: ReactNode }) {
  const { state, dispatch } = useEditorState();
  const { zoom, setDragging } = useCanvasZoom();
  const [draggingType, setDraggingType] = useState<string | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor),
  );

  function handleDragStart(event: DragStartEvent) {
    // The canvas stops accepting zoom gestures for the duration: dnd-kit measures
    // its drop targets once, at the start, and resizing them underneath a live
    // drag leaves the block landing somewhere other than where it was let go.
    setDragging(true);
    const data = event.active.data.current as DragData | undefined;
    if (data?.source === "palette") {
      setDraggingType(data.blockType);
    } else {
      const block = findBlock(state.document, String(event.active.id));
      setDraggingType(block?.type ?? null);
    }
  }

  function handleDragEnd(event: DragEndEvent) {
    setDraggingType(null);
    setDragging(false);
    const { active, over } = event;
    if (!over) return;
    const data = active.data.current as DragData | undefined;
    const target = over.data.current as DropData | undefined;
    if (!data || !target) return;
    const { flowId, index } = target;

    if (data.source === "palette") {
      dispatch({
        type: EditorActionType.ADD_BLOCK,
        data: { blockType: data.blockType, flowId, index },
      });
      return;
    }

    // Moving an existing block across flows (or nested slots).
    if (data.flowId !== flowId) {
      dispatch({
        type: EditorActionType.MOVE_BLOCK_ACROSS,
        data: {
          fromFlowId: data.flowId,
          toFlowId: flowId,
          blockId: String(active.id),
          index,
        },
      });
      return;
    }

    // Reordering within the same flow: translate the gap index into a move.
    const flow = findFlow(state.document, flowId);
    if (!flow) return;
    const from = flow.process.findIndex((b) => b.id === active.id);
    if (from === -1 || index === from || index === from + 1) return;
    const to = index > from ? index - 1 : index;
    dispatch({
      type: EditorActionType.MOVE_BLOCK,
      data: { flowId, fromIndex: from, toIndex: to },
    });
  }

  return (
    <DndContext
      sensors={sensors}
      // Pointer first, nearest-centre as the fallback. The overlay is rendered
      // outside the zoomed layer, so comparing its box against drop targets drawn
      // at another scale drifts by half a card at 25%; where the pointer is does
      // not depend on scale at all. The fallback still matters — the gaps are
      // thin, and a pointer between two of them hits neither.
      collisionDetection={(args) => {
        const hit = pointerWithin(args);
        return hit.length ? hit : closestCenter(args);
      }}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
      onDragCancel={() => {
        setDraggingType(null);
        setDragging(false);
      }}
    >
      {children}
      {/* The overlay is a sibling of the canvas rather than a child of it, so it
          is not inside the zoomed layer and would float a full-size preview over
          half-size cards. `zoom` rather than a transform so the overlay's own box
          and its contents agree about how big they are. */}
      <DragOverlay dropAnimation={null}>
        {draggingType ? (
          <div style={{ zoom }}>
            <DragPreview blockType={draggingType} />
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}
