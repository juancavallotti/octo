import { useEffect } from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorActionType, EditorStateProvider, useEditorState } from "./editorState";
import { useNewItem, type NewItemKind } from "./useNewItem";
import { parseProject } from "../model/project";
import FileSwitcher from "../components/FileSwitcher";
import TestingView from "../components/testing/TestingView";
import { TestSuiteProvider, type TestSuiteStore } from "../providers/TestSuiteProvider";

const files = parseProject([
  { path: "octo.yaml", content: "service:\n  name: shop\n" },
  { path: "orders.yaml", content: "flows:\n  - name: intake\n    process: []\n" },
]);

/** Loads the project, then offers one button per kind, as a host's menu would. */
function Host() {
  const { state, dispatch } = useEditorState();
  const newItem = useNewItem();
  useEffect(() => {
    dispatch({ type: EditorActionType.LOAD_PROJECT, data: { id: "p", name: "shop", folderId: null, files } });
  }, [dispatch]);
  const kinds: NewItemKind[] = ["flow", "file", "resource", "suite"];
  return (
    <>
      {kinds.map((k) => (
        <button key={k} onClick={() => newItem(k)}>
          new {k}
        </button>
      ))}
      <button onClick={() => dispatch({ type: EditorActionType.UNDO })}>undo</button>
      <p data-testid="flows">{state.document.flows.map((f) => f.name).join(",")}</p>
      <p data-testid="view">{`${state.viewMode}/${state.flowsView}`}</p>
    </>
  );
}

function suiteStore() {
  const saved: string[] = [];
  const store: TestSuiteStore = {
    list: async () => [],
    save: async (_id, flow) => {
      saved.push(flow);
    },
    remove: async () => {},
    canEdit: () => true,
  };
  return { store, saved };
}

describe("useNewItem", () => {
  it("adds a flow to the open file on the canvas, as one undoable edit", async () => {
    const user = userEvent.setup();
    render(
      <EditorStateProvider>
        <Host />
      </EditorStateProvider>,
    );
    await user.click(screen.getByText("new flow"));
    expect(screen.getByTestId("flows")).toHaveTextContent("intake,flow-2");
    expect(screen.getByTestId("view")).toHaveTextContent("flows/canvas");
    await user.click(screen.getByText("undo"));
    expect(screen.getByTestId("flows")).toHaveTextContent(/^intake$/);
  });

  it("opens the file menu on a new file's name", async () => {
    const user = userEvent.setup();
    render(
      <EditorStateProvider>
        <Host />
        <FileSwitcher />
      </EditorStateProvider>,
    );
    await user.click(screen.getByText("new file"));
    expect(screen.getByLabelText("New file name")).toHaveFocus();
  });

  it("starts a suite for the current flow on the Testing tab", async () => {
    const user = userEvent.setup();
    const { store } = suiteStore();
    render(
      <EditorStateProvider>
        <Host />
        <TestSuiteProvider store={store}>
          <TestingView />
        </TestSuiteProvider>
      </EditorStateProvider>,
    );
    await user.click(screen.getByText("new suite"));
    expect(screen.getByTestId("view")).toHaveTextContent(/^testing\//);
    expect(await screen.findByText("intake_test.yaml")).toBeInTheDocument();
    // A starter was written, not just the empty "no tests yet" invitation shown.
    expect(screen.queryByText('No tests for "intake" yet')).toBeNull();
  });
});
