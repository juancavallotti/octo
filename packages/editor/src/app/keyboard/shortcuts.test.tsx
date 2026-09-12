import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorActionType, EditorStateProvider, useEditorState } from "../state/editorState";
import { useHistoryShortcuts } from "./useHistoryShortcuts";
import { useSelectionShortcuts } from "./useSelectionShortcuts";

/**
 * The shortcuts are tested through the real reducer rather than against a mock
 * dispatch, because what is worth proving is the round trip: that Delete finds the
 * flow the selected block is actually in, and that undo puts it back where it was.
 * A test that asserted "an action was dispatched" would pass with the wrong flow id.
 */
function Harness() {
  const { state, dispatch, canUndo } = useEditorState();
  useHistoryShortcuts();
  useSelectionShortcuts();

  const flow = state.document.flows[0];
  return (
    <div>
      <button onClick={() => dispatch({ type: EditorActionType.ADD_BLOCK, data: { blockType: "log" } })}>
        add
      </button>
      <p data-testid="blocks">{flow ? flow.process.map((b) => b.type).join(",") : ""}</p>
      <p data-testid="selected">{state.selectedBlockId ?? "none"}</p>
      <p data-testid="undo">{canUndo ? "yes" : "no"}</p>
      <input aria-label="a field" />
    </div>
  );
}

function renderHarness() {
  return render(
    <EditorStateProvider>
      <Harness />
    </EditorStateProvider>,
  );
}

const blocks = () => screen.getByTestId("blocks").textContent;
const selected = () => screen.getByTestId("selected").textContent;

describe("selection shortcuts", () => {
  it("deletes the selected block", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByText("add"));
    expect(blocks()).toBe("log");

    await user.keyboard("{Delete}");
    expect(blocks()).toBe("");
  });

  it("treats Backspace the same way", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByText("add"));
    await user.keyboard("{Backspace}");
    expect(blocks()).toBe("");
  });

  it("clears the selection on Escape without deleting anything", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByText("add"));
    expect(selected()).not.toBe("none");

    await user.keyboard("{Escape}");
    expect(selected()).toBe("none");
    expect(blocks()).toBe("log");
  });

  it("does nothing once nothing is selected", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByText("add"));
    await user.keyboard("{Escape}");
    await user.keyboard("{Delete}");
    expect(blocks()).toBe("log");
  });

  it("leaves Backspace alone while the user is typing", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByText("add"));
    await user.click(screen.getByLabelText("a field"));
    await user.keyboard("abc{Backspace}");

    expect(screen.getByLabelText("a field")).toHaveValue("ab");
    expect(blocks()).toBe("log");
  });
});

describe("history shortcuts", () => {
  it("takes back a delete and restores the selection with it", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByText("add"));
    const added = selected();

    await user.keyboard("{Delete}");
    expect(blocks()).toBe("");

    await user.keyboard("{Meta>}z{/Meta}");
    expect(blocks()).toBe("log");
    // The selection travels with the snapshot, which is what makes an undone delete
    // leave the canvas as it was rather than merely as it looked.
    expect(selected()).toBe(added);
  });

  it("redoes with shift", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByText("add"));
    await user.keyboard("{Meta>}z{/Meta}");
    expect(blocks()).toBe("");

    await user.keyboard("{Meta>}{Shift>}z{/Shift}{/Meta}");
    expect(blocks()).toBe("log");
  });

  it("has nothing to undo before the first edit", () => {
    renderHarness();
    expect(screen.getByTestId("undo")).toHaveTextContent("no");
  });

  it("does not steal Cmd+Z from a text field", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByText("add"));
    await user.click(screen.getByLabelText("a field"));
    await user.keyboard("{Meta>}z{/Meta}");

    // The browser owns undo inside an input; the document must be untouched.
    expect(blocks()).toBe("log");
  });
});
