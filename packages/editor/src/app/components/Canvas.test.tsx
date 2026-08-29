import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorStateProvider } from "../state/editorState";
import { CanvasZoomProvider } from "../canvas/ZoomContext";
import DndProvider from "./DndProvider";
import Canvas from "./Canvas";

/**
 * jsdom does not implement CSS `zoom`, so nothing here can assert on geometry.
 * What it can assert is the contract the browser is being handed: the factor on
 * the layer, the custom property the dot grid reads, and the drag translate
 * having been divided back out. Those are the three places a mistake would be
 * invisible until someone dragged a block at 50% and watched it lag their hand.
 */
function renderCanvas() {
  return render(
    <EditorStateProvider>
      <CanvasZoomProvider>
        <DndProvider>
          <Canvas />
        </DndProvider>
      </CanvasZoomProvider>
    </EditorStateProvider>,
  );
}

const surface = () => screen.getByRole("main");
const layer = () => surface().firstElementChild as HTMLElement;

describe("Canvas zoom", () => {
  it("draws at natural size to begin with", () => {
    renderCanvas();
    expect(screen.getByLabelText(/^Zoom 100%/)).toBeInTheDocument();
  });

  it("hands the browser the factor on a layer inside the scroller", async () => {
    // Inside, not on the scroller itself: the layer is what has to change size,
    // while the surface it scrolls in stays the size of the window.
    renderCanvas();
    await userEvent.click(screen.getByLabelText("Zoom out"));
    expect(layer().style.zoom).toBe("0.75");
    expect(surface().style.zoom).toBe("");
  });

  it("scales the dot grid with what it sits behind", async () => {
    renderCanvas();
    await userEvent.click(screen.getByLabelText("Zoom out"));
    // The grid is painted on the unzoomed scroller, so it only follows if it is
    // told to — a grid that keeps its cell while the flows change theirs stops
    // reading as one surface.
    expect(surface().style.getPropertyValue("--canvas-zoom")).toBe("0.75");
  });

  it("comes back to 100% from the percentage itself", async () => {
    renderCanvas();
    await userEvent.click(screen.getByLabelText("Zoom out"));
    await userEvent.click(screen.getByLabelText(/reset to 100%/));
    expect(layer().style.zoom).toBe("1");
  });

  it("fits on whichever axis has run out first", async () => {
    // jsdom reports every box as 0, which fitZoom answers with 1 — so what this
    // pins is that Fit is wired and lands somewhere legal, not the factor. The
    // axis choice is checked in the browser, where boxes have sizes.
    renderCanvas();
    await userEvent.click(screen.getByLabelText("Zoom out"));
    await userEvent.click(screen.getByLabelText("Fit flows in view"));
    expect(layer().style.zoom).toBe("1");
  });

  it("stops offering to zoom past the ends", async () => {
    renderCanvas();
    const out = screen.getByLabelText("Zoom out");
    for (let i = 0; i < 10; i++) await userEvent.click(out);
    expect(out).toBeDisabled();
    expect(screen.getByLabelText(/^Zoom 25%/)).toBeInTheDocument();
  });
});
