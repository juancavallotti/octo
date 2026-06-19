/**
 * Shared drag-and-drop constants and payload types. The palette (drag sources),
 * the canvas blocks (drag sources), and the insertion gaps (drop targets) all
 * reference these so the central DndProvider can tell a palette-add from a move
 * and route it to the right flow and index.
 */

/** Droppable id for an insertion gap: "insert at `index` of `flowId`". */
export function gapId(flowId: string, index: number): string {
  return `gap-${flowId}-${index}`;
}

/** Dragging a block type out of the palette to add it. */
export interface PaletteDragData {
  source: "palette";
  blockType: string;
}

/** Dragging an existing block (the drag id is the block id) to move it. */
export interface CanvasDragData {
  source: "canvas";
  flowId: string;
}

export type DragData = PaletteDragData | CanvasDragData;

/** Data attached to every insertion gap: the flow and the index to insert at. */
export interface DropData {
  flowId: string;
  index: number;
}
