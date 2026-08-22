import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import ThinkingSegment from "./ThinkingSegment";

describe("ThinkingSegment", () => {
  it("shows nothing at all when there is no reasoning", () => {
    const { container } = render(<ThinkingSegment text="" streaming answered={false} />);

    expect(container).toBeEmptyDOMElement();
  });

  // While this is the only thing happening it should be readable without a click:
  // it is what tells the reader the model is working rather than hung.
  it("is open while the model is still thinking", () => {
    render(<ThinkingSegment text="weighing it up" streaming answered={false} />);

    expect(screen.getByText("Thinking…")).toBeTruthy();
    expect(screen.getByText("weighing it up")).toBeTruthy();
  });

  it("collapses once the answer starts, keeping a size to hint at what is behind it", () => {
    render(<ThinkingSegment text="weighing it up" streaming answered />);

    expect(screen.queryByText("weighing it up")).toBeNull();
    expect(screen.getByText("Thought about it")).toBeTruthy();
    expect(screen.getByText(/14 chars/)).toBeTruthy();
  });

  // The answer arriving must not shut a panel someone deliberately opened to read.
  it("respects a deliberate choice over the answer arriving", async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <ThinkingSegment text="weighing it up" streaming answered={false} />,
    );

    // Close it, then open it again — now it is a choice rather than the default.
    await user.click(screen.getByRole("button"));
    await user.click(screen.getByRole("button"));
    expect(screen.getByText("weighing it up")).toBeTruthy();

    rerender(<ThinkingSegment text="weighing it up" streaming answered />);
    expect(screen.getByText("weighing it up")).toBeTruthy();
  });

  it("can be collapsed while it is still streaming", async () => {
    const user = userEvent.setup();
    render(<ThinkingSegment text="weighing it up" streaming answered={false} />);

    await user.click(screen.getByRole("button"));

    expect(screen.queryByText("weighing it up")).toBeNull();
  });
});
