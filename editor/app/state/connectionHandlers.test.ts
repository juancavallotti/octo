import { describe, it, expect } from "vitest";
import { blankDocument } from "@/app/model/document";
import type { EditorState } from "./reducer";
import {
  addConnection,
  removeConnection,
  renameConnection,
  selectConnection,
  updateConnectionSetting,
} from "./connectionHandlers";

function baseState(): EditorState {
  return {
    document: blankDocument(),
    activeFlowId: null,
    selectedBlockId: null,
    selectedSourceFlowId: null,
    selectedConnectionId: null,
    selectedComponentId: null,
  };
}

describe("connectionHandlers", () => {
  it("adds a connection with a unique slug name and selects it", () => {
    let state = addConnection(baseState(), { type: "database" });
    expect(state.document.connectors).toHaveLength(1);
    expect(state.document.connectors[0].name).toBe("database");
    expect(state.selectedConnectionId).toBe(state.document.connectors[0].id);

    state = addConnection(state, { type: "database" });
    expect(state.document.connectors.map((c) => c.name)).toEqual([
      "database",
      "database-2",
    ]);
  });

  it("renames and updates settings of a connection", () => {
    let state = addConnection(baseState(), { type: "database" });
    const id = state.document.connectors[0].id;

    state = renameConnection(state, { id, name: "primary-db" });
    expect(state.document.connectors[0].name).toBe("primary-db");

    state = updateConnectionSetting(state, { id, field: "dsn", value: "x" });
    expect(state.document.connectors[0].settings.dsn).toBe("x");
  });

  it("clears selection when the selected connection is removed", () => {
    let state = addConnection(baseState(), { type: "database" });
    const id = state.document.connectors[0].id;

    state = removeConnection(state, { id });
    expect(state.document.connectors).toHaveLength(0);
    expect(state.selectedConnectionId).toBeNull();
  });

  it("select clears block and source selection", () => {
    let state = addConnection(baseState(), { type: "database" });
    const id = state.document.connectors[0].id;
    state = { ...state, selectedBlockId: "b", selectedSourceFlowId: "f" };

    state = selectConnection(state, { id });
    expect(state.selectedConnectionId).toBe(id);
    expect(state.selectedBlockId).toBeNull();
    expect(state.selectedSourceFlowId).toBeNull();
  });
});
