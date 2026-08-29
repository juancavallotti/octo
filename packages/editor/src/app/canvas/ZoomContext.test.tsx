import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CanvasZoomProvider, useCanvasZoom } from "./ZoomContext";

function Probe() {
  const { zoom, zoomIn, zoomOut, reset, setZoom, setDragging } = useCanvasZoom();
  return (
    <div>
      <span data-testid="zoom">{zoom}</span>
      <button type="button" onClick={() => setDragging(true)}>
        start drag
      </button>
      <button type="button" onClick={zoomIn}>
        in
      </button>
      <button type="button" onClick={zoomOut}>
        out
      </button>
      <button type="button" onClick={reset}>
        reset
      </button>
      <button type="button" onClick={() => setZoom(99)}>
        far
      </button>
    </div>
  );
}

const zoom = () => screen.getByTestId("zoom").textContent;

describe("CanvasZoomProvider", () => {
  it("starts at natural size", () => {
    // Which is also what keeps the preview route's screenshots stable: nothing
    // is persisted, so every mount draws the same picture.
    render(
      <CanvasZoomProvider>
        <Probe />
      </CanvasZoomProvider>,
    );
    expect(zoom()).toBe("1");
  });

  it("steps out and back to exactly where it started", async () => {
    const user = userEvent.setup();
    render(
      <CanvasZoomProvider>
        <Probe />
      </CanvasZoomProvider>,
    );

    await user.click(screen.getByText("out"));
    await user.click(screen.getByText("out"));
    expect(zoom()).toBe("0.67");
    await user.click(screen.getByText("in"));
    await user.click(screen.getByText("in"));
    expect(zoom()).toBe("1");
  });

  it("refuses a factor outside the range", async () => {
    const user = userEvent.setup();
    render(
      <CanvasZoomProvider>
        <Probe />
      </CanvasZoomProvider>,
    );

    await user.click(screen.getByText("far"));
    expect(zoom()).toBe("2");
  });

  it("refuses to change while a block is being dragged", async () => {
    // dnd-kit measures its drop targets once, at the start. A keyboard drag
    // leaves the pointer free to reach the buttons, so the rule has to live here
    // rather than at each of the four ways in.
    const user = userEvent.setup();
    render(
      <CanvasZoomProvider>
        <Probe />
      </CanvasZoomProvider>,
    );

    await user.click(screen.getByText("start drag"));
    await user.click(screen.getByText("out"));
    await user.click(screen.getByText("far"));

    expect(zoom()).toBe("1");
  });

  it("draws at natural size with no provider at all", () => {
    // A component rendered on its own — a test, a story — should draw itself
    // rather than fail for want of a canvas around it.
    render(<Probe />);
    expect(zoom()).toBe("1");
  });
});
