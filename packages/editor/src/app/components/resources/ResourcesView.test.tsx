import { useEffect } from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorActionType, EditorStateProvider, useEditorState } from "../../state/editorState";
import { projectFiles } from "../../state/projectHandlers";
import { parseProject } from "../../model/project";
import {
  ResourceStoreProvider,
  type ResourceStore,
  type StoredResource,
} from "../../providers/ResourceStoreProvider";
import ResourcesView from "./ResourcesView";

// The env file is declared in octo.yaml; the file left open is orders.yaml.
const files = parseProject([
  { path: "octo.yaml", content: "service:\n  name: shop\nresources:\n  env: [.env.staging]\n" },
  { path: "orders.yaml", content: "flows:\n  - name: intake\n    process: []\n" },
]);

const stored: StoredResource[] = [
  { id: "r1", name: ".env.staging", kind: "env", content: "A=1\n" },
];

const store = { list: async () => stored } as unknown as ResourceStore;

/** Loads the project and reports what octo.yaml declares now. */
function Host() {
  const { state, dispatch } = useEditorState();
  useEffect(() => {
    dispatch({ type: EditorActionType.LOAD_PROJECT, data: { id: "p", name: "shop", folderId: null, files } });
  }, [dispatch]);
  const octo = projectFiles(state).find((f) => f.path === "octo.yaml");
  return (
    <>
      <p data-testid="active">{state.activePath}</p>
      <p data-testid="octo-env">{octo?.document.resources?.env.join(",")}</p>
    </>
  );
}

function renderView() {
  return render(
    <EditorStateProvider>
      <Host />
      <ResourceStoreProvider value={store}>
        <ResourcesView />
      </ResourceStoreProvider>
    </EditorStateProvider>,
  );
}

describe("ResourcesView", () => {
  it("counts a resource declared in a file that is not open as declared", async () => {
    renderView();
    expect(await screen.findByTitle(".env.staging")).toBeInTheDocument();
    expect(screen.getByTestId("active")).toHaveTextContent("orders.yaml");
    expect(screen.queryByTitle(/not in project/)).toBeNull();
  });

  it("removes the declaration from the file that holds it", async () => {
    const user = userEvent.setup();
    renderView();
    await screen.findByTitle(".env.staging");
    await user.click(screen.getByRole("button", { name: "Remove from project (keep file)" }));
    expect(screen.getByTestId("octo-env")).toBeEmptyDOMElement();
    expect(await screen.findByTitle(".env.staging — not in project")).toBeInTheDocument();
  });
});
