import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorActionType, EditorStateProvider, useEditorState } from "../state/editorState";
import HistoryButtons from "./HistoryButtons";

function Harness() {
  const { state, dispatch } = useEditorState();
  const flow = state.document.flows[0];
  return (
    <div>
      <HistoryButtons />
      <button onClick={() => dispatch({ type: EditorActionType.ADD_BLOCK, data: { blockType: "log" } })}>
        add
      </button>
      <p data-testid="blocks">{flow ? flow.process.map((b) => b.type).join(",") : ""}</p>
    </div>
  );
}

const renderBar = () =>
  render(
    <EditorStateProvider>
      <Harness />
    </EditorStateProvider>,
  );

const blocks = () => screen.getByTestId("blocks").textContent;
const undo = () => screen.getByRole("button", { name: "Undo" });
const redo = () => screen.getByRole("button", { name: "Redo" });

describe("HistoryButtons", () => {
  it("is visible but disabled before there is anything to undo", () => {
    // Disabled rather than absent: undo is looked for at the moment it is needed,
    // and a control that appears only once it works cannot be found beforehand.
    renderBar();
    expect(undo()).toBeDisabled();
    expect(redo()).toBeDisabled();
  });

  it("undoes an edit", async () => {
    const user = userEvent.setup();
    renderBar();
    await user.click(screen.getByText("add"));
    expect(blocks()).toBe("log");

    expect(undo()).toBeEnabled();
    await user.click(undo());
    expect(blocks()).toBe("");
  });

  it("redoes what it undid, and only then", async () => {
    const user = userEvent.setup();
    renderBar();
    await user.click(screen.getByText("add"));
    expect(redo()).toBeDisabled();

    await user.click(undo());
    expect(redo()).toBeEnabled();
    await user.click(redo());
    expect(blocks()).toBe("log");
  });

  it("names the keyboard shortcut, so the button teaches it", async () => {
    renderBar();
    expect(undo().title).toMatch(/Undo \((⌘|Ctrl\+)Z\)/);
  });
});
