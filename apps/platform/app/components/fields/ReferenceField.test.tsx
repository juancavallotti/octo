import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { EditorDocument } from "@/app/model/document";
import ReferenceField from "./ReferenceField";

const doc: EditorDocument = {
  connectors: [
    { id: "c1", name: "main-http", type: "http", settings: {} },
    { id: "c2", name: "api-client", type: "http-client", settings: {} },
    { id: "c3", name: "other-client", type: "http-client", settings: {} },
    { id: "c4", name: "claude", type: "llm-anthropic", settings: {} },
    { id: "c5", name: "gpt", type: "llm-openai", settings: {} },
  ],
  flows: [
    { id: "f1", name: "main", process: [] },
    { id: "f2", name: "worker", process: [] },
  ],
  processors: [],
  env: [],
};

vi.mock("@/app/state/editorState", () => ({
  useEditorState: () => ({ state: { document: doc }, dispatch: () => {} }),
}));

describe("ReferenceField", () => {
  it("renders a dropdown of connections of the matching connector type", () => {
    render(
      <ReferenceField
        spec={{ kind: "connector", connectorType: "http-client" }}
        value=""
        required={false}
        onChange={() => {}}
      />,
    );
    expect(screen.getByRole("combobox")).toBeInTheDocument();
    // Only http-client connections, plus the default option.
    expect(screen.getByRole("option", { name: "api-client" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "other-client" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "main-http" })).toBeNull();
    expect(screen.getByRole("option", { name: "— (default)" })).toBeInTheDocument();
  });

  it("renders any connector in the category for a category reference", () => {
    render(
      <ReferenceField
        spec={{ kind: "connector", connectorCategory: "llm" }}
        value=""
        required={false}
        onChange={() => {}}
      />,
    );
    // Every llm-* provider, regardless of exact type; nothing outside the category.
    expect(screen.getByRole("option", { name: "claude" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "gpt" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "api-client" })).toBeNull();
  });

  it("renders a dropdown of flow names for a flow reference", () => {
    render(
      <ReferenceField
        spec={{ kind: "flow" }}
        value=""
        required
        onChange={() => {}}
      />,
    );
    expect(screen.getByRole("option", { name: "main" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "worker" })).toBeInTheDocument();
  });

  it("surfaces a value that no longer resolves as missing", () => {
    render(
      <ReferenceField
        spec={{ kind: "connector", connectorType: "http-client" }}
        value="deleted-conn"
        required={false}
        onChange={() => {}}
      />,
    );
    expect(
      screen.getByRole("option", { name: "deleted-conn (missing)" }),
    ).toBeInTheDocument();
  });
});
