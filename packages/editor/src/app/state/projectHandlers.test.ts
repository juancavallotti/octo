import { describe, it, expect } from "vitest";
import { blankDocument, emptyDocument, emptyFlow, newBlock } from "../model/document";
import type { ProjectFile } from "../model/project";
import { EditorActionType } from "./actions";
import { initialState, reducer, type EditorState } from "./reducer";
import { projectFiles } from "./projectHandlers";

function file(path: string, flowName?: string, declaresService = false): ProjectFile {
  const document = flowName
    ? { ...blankDocument(), flows: [emptyFlow(flowName)] }
    : blankDocument();
  return { path, document, declaresService };
}

/** A loaded project: a project file of connections, and two files of flows. */
function loaded(): EditorState {
  return reducer(initialState, {
    type: EditorActionType.LOAD_PROJECT,
    data: {
      id: "p1",
      name: "Shop",
      folderId: null,
      files: [file("orders.yaml", "orders"), file("octo.yaml", undefined, true), file("billing.yaml", "billing")],
    },
  });
}

function addBlock(state: EditorState): EditorState {
  return reducer(state, { type: EditorActionType.ADD_BLOCK, data: { blockType: "log" } });
}

describe("project metadata", () => {
  it("starts with empty project metadata and one file", () => {
    expect(initialState.project).toEqual({ id: null, name: "", folderId: null });
    expect(initialState.files.map((f) => f.path)).toEqual(["octo.yaml"]);
  });

  it("sets the title, id and folder without touching the document", () => {
    let state = loaded();
    const doc = state.document;
    state = reducer(state, { type: EditorActionType.SET_PROJECT_TITLE, data: { name: "Store" } });
    state = reducer(state, { type: EditorActionType.SET_PROJECT_ID, data: { id: "p9" } });
    state = reducer(state, { type: EditorActionType.SET_PROJECT_FOLDER, data: { folderId: "f1" } });
    expect(state.project).toEqual({ id: "p9", name: "Store", folderId: "f1" });
    expect(state.document).toBe(doc);
  });
});

describe("loading a project", () => {
  it("opens the first file by name that holds a flow, not the project file", () => {
    const state = loaded();
    expect(state.activePath).toBe("billing.yaml");
    expect(state.document.flows[0].name).toBe("billing");
    expect(state.activeFlowId).toBe(state.document.flows[0].id);
  });

  it("opens the file asked for", () => {
    const state = reducer(initialState, {
      type: EditorActionType.LOAD_PROJECT,
      data: { id: "p1", name: "Shop", folderId: null, files: [file("a.yaml", "a"), file("b.yaml", "b")], activePath: "b.yaml" },
    });
    expect(state.activePath).toBe("b.yaml");
  });

  it("starts a new project with one file that declares the service", () => {
    const state = reducer(loaded(), { type: EditorActionType.NEW_PROJECT });
    expect(state.project).toEqual({ id: null, name: "", folderId: null });
    expect(state.files).toEqual([
      { path: "octo.yaml", document: state.document, declaresService: true },
    ]);
    expect(state.document.flows).toHaveLength(1);
  });
});

describe("switching files", () => {
  it("keeps an unsaved edit when switching away and back", () => {
    let state = addBlock(loaded());
    const edited = state.document;
    state = reducer(state, { type: EditorActionType.SWITCH_FILE, data: { path: "orders.yaml" } });
    expect(state.document.flows[0].name).toBe("orders");
    state = reducer(state, { type: EditorActionType.SWITCH_FILE, data: { path: "billing.yaml" } });
    expect(state.document).toBe(edited);
  });

  it("reads the project with the active file's current document", () => {
    const state = addBlock(loaded());
    const billing = projectFiles(state).find((f) => f.path === "billing.yaml")!;
    expect(billing.document).toBe(state.document);
    expect(projectFiles(state).map((f) => f.path)).toEqual(["billing.yaml", "octo.yaml", "orders.yaml"]);
  });

  it("ignores a switch to a file the project does not have", () => {
    const state = loaded();
    expect(reducer(state, { type: EditorActionType.SWITCH_FILE, data: { path: "nope.yaml" } })).toBe(state);
  });
});

describe("creating, renaming and deleting files", () => {
  it("creates a file and opens it", () => {
    const state = reducer(loaded(), { type: EditorActionType.CREATE_FILE, data: { path: "shipping.yaml" } });
    expect(state.activePath).toBe("shipping.yaml");
    expect(state.files.find((f) => f.path === "shipping.yaml")?.declaresService).toBe(false);
    expect(state.document.flows).toHaveLength(1);
  });

  it("names a new file's flow apart from every flow in the project", () => {
    let state = reducer(loaded(), { type: EditorActionType.CREATE_FILE, data: { path: "a.yaml" } });
    state = reducer(state, { type: EditorActionType.CREATE_FILE, data: { path: "b.yaml" } });
    const names = projectFiles(state).flatMap((f) => f.document.flows.map((fl) => fl.name));
    expect(new Set(names).size).toBe(names.length);
  });

  it.each([["orders.yaml"], ["nested/x.yaml"], ["orders_test.yaml"], ["notes.md"]])(
    "refuses to create %s",
    (path) => {
      const state = loaded();
      expect(reducer(state, { type: EditorActionType.CREATE_FILE, data: { path } })).toBe(state);
    },
  );

  it("renames the active file and keeps its edits", () => {
    let state = addBlock(loaded());
    const edited = state.document;
    state = reducer(state, { type: EditorActionType.RENAME_FILE, data: { from: "billing.yaml", to: "invoices.yaml" } });
    expect(state.activePath).toBe("invoices.yaml");
    expect(state.document).toBe(edited);
    expect(state.files.map((f) => f.path)).toEqual(["invoices.yaml", "octo.yaml", "orders.yaml"]);
  });

  it("refuses a rename onto a taken name", () => {
    const state = loaded();
    const next = reducer(state, { type: EditorActionType.RENAME_FILE, data: { from: "billing.yaml", to: "orders.yaml" } });
    expect(next).toBe(state);
  });

  it("deletes the active file and opens another", () => {
    const state = reducer(loaded(), { type: EditorActionType.DELETE_FILE, data: { path: "billing.yaml" } });
    expect(state.files.map((f) => f.path)).toEqual(["octo.yaml", "orders.yaml"]);
    expect(state.activePath).toBe("octo.yaml");
  });

  it("keeps the last file", () => {
    const one = reducer(initialState, { type: EditorActionType.NEW_PROJECT });
    expect(reducer(one, { type: EditorActionType.DELETE_FILE, data: { path: "octo.yaml" } })).toBe(one);
  });
});

describe("flows", () => {
  it("names an added flow apart from the flows in other files", () => {
    // billing.yaml holds one flow, so the next by its own count is flow-2 — taken in orders.yaml.
    let state = reducer(loaded(), { type: EditorActionType.SWITCH_FILE, data: { path: "orders.yaml" } });
    state = reducer(state, { type: EditorActionType.RENAME_FLOW, data: { flowId: state.document.flows[0].id, name: "flow-2" } });
    state = reducer(state, { type: EditorActionType.SWITCH_FILE, data: { path: "billing.yaml" } });
    state = reducer(state, { type: EditorActionType.ADD_FLOW });
    expect(state.document.flows.map((f) => f.name)).toEqual(["billing", "flow-3"]);
  });
});

describe("blocks", () => {
  it("edits only the active file", () => {
    const state = addBlock(loaded());
    const orders = state.files.find((f) => f.path === "orders.yaml")!;
    expect(orders.document.flows[0].process).toHaveLength(0);
    expect(state.document.flows[0].process.map((b) => b.type)).toEqual([newBlock("log").type]);
    expect(emptyDocument().flows).toHaveLength(1);
  });
});
