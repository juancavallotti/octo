import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { EditorStateProvider } from "../state/editorState";
import { CanvasZoomProvider, useCanvasZoom } from "../canvas/ZoomContext";
import type { BlockNode } from "../model/document";
import NodeShell from "./NodeShell";

/**
 * The one piece of zoom that jsdom can be made to check.
 *
 * dnd-kit reports a drag delta in screen pixels. NodeShell applies it inside the
 * layer the browser is already scaling, so the browser multiplies it a second
 * time: at 50% an uncorrected node travels half as far as the hand holding it,
 * which is the kind of wrongness that looks like lag rather than like a bug.
 * jsdom implements neither `zoom` nor a pointer drag, so the transform string is
 * mocked in at the seam and read back out.
 */
const DELTA = { x: 40, y: 20, scaleX: 1, scaleY: 1 };

vi.mock("@dnd-kit/core", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@dnd-kit/core")>()),
  useDraggable: () => ({
    attributes: {},
    listeners: {},
    setNodeRef: () => {},
    transform: DELTA,
    isDragging: true,
  }),
}));

const BLOCK: BlockNode = { id: "b1", type: "log", name: "Log", config: {} };

/** Sets the zoom from inside the provider, since that is the only way in. */
function AtZoom({ zoom }: { zoom: number }) {
  const { setZoom } = useCanvasZoom();
  if (zoom !== 1) setZoom(zoom);
  return null;
}

function renderNode(zoom: number) {
  render(
    <EditorStateProvider>
      <CanvasZoomProvider>
        <AtZoom zoom={zoom} />
        <NodeShell block={BLOCK} flowId="f1" icon={null} label="Log" />
      </CanvasZoomProvider>
    </EditorStateProvider>,
  );
  return screen.getByLabelText("Drag to reorder").closest("[style]") as HTMLElement;
}

describe("NodeShell under zoom", () => {
  it("moves with the cursor at natural size", () => {
    expect(renderNode(1).style.transform).toBe("translate3d(40px, 20px, 0)");
  });

  it("divides the delta back out when the layer is scaled", () => {
    expect(renderNode(0.5).style.transform).toBe("translate3d(80px, 40px, 0)");
  });
});
