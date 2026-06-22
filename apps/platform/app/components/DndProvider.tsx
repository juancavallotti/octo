"use client";

import { ReactNode, useState } from "react";
import {
  closestCenter,
  DndContext,
  DragEndEvent,
  DragOverlay,
  DragStartEvent,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { findBlock, findFlow } from "@/app/model/document";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import { DragData, DropData } from "./dnd";
import DragPreview from "./DragPreview";

/**
 * A single DndContext spanning the editor body so the palette (drag sources) and
 * every flow's blocks (drag sources) share one drag session with the insertion
 * gaps (drop targets). onDragEnd is the one place a drop becomes a reducer
 * action: a palette drag inserts a new block at the gap's index; a canvas drag
 * reorders within its flow or moves across flows (including into nested slots).
 */
export default function DndProvider({ children }: { children: ReactNode }) {
  const { state, dispatch } = useEditorState();
  const [draggingType, setDraggingType] = useState<string | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor),
  );

  function handleDragStart(event: DragStartEvent) {
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
      collisionDetection={closestCenter}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
      onDragCancel={() => setDraggingType(null)}
    >
      {children}
      <DragOverlay dropAnimation={null}>
        {draggingType ? <DragPreview blockType={draggingType} /> : null}
      </DragOverlay>
    </DndContext>
  );
}
