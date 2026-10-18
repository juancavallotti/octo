import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorStateProvider, useEditorState, EditorActionType } from "../state/editorState";
import FlowsViewToggle from "./FlowsViewToggle";

function Probe() {
  const { state, dispatch } = useEditorState();
  return (
    <>
      <p data-testid="view">{state.viewMode}</p>
      <button onClick={() => dispatch({ type: EditorActionType.SET_VIEW_MODE, data: "testing" })}>
        to testing
      </button>
      <button onClick={() => dispatch({ type: EditorActionType.SET_VIEW_MODE, data: "flows" })}>
        to flows
      </button>
    </>
  );
}

function renderToggle() {
  return render(
    <EditorStateProvider>
      <FlowsViewToggle />
      <Probe />
    </EditorStateProvider>,
  );
}

describe("FlowsViewToggle", () => {
  it("starts on Canvas and switches to YAML on click", async () => {
    renderToggle();
    const canvas = screen.getByRole("button", { name: "Canvas" });
    const yaml = screen.getByRole("button", { name: "YAML" });

    expect(canvas).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(yaml);
    expect(yaml).toHaveAttribute("aria-pressed", "true");
    expect(canvas).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByTestId("view")).toHaveTextContent("flows");
  });

  // Leaving for another tab and coming back returns to the file as it was shown.
  it("keeps YAML across a visit to another tab", async () => {
    renderToggle();
    await userEvent.click(screen.getByRole("button", { name: "YAML" }));
    await userEvent.click(screen.getByText("to testing"));
    await userEvent.click(screen.getByText("to flows"));
    expect(screen.getByRole("button", { name: "YAML" })).toHaveAttribute("aria-pressed", "true");
  });
});
