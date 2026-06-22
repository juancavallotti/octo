import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { EditorDocument } from "@/app/model/document";
import EnvValueField, { isEnvRef } from "./EnvValueField";

const doc: EditorDocument = {
  connectors: [],
  flows: [],
  processors: [],
  env: [{ name: "WEATHER_LAT" }, { name: "PORT", default: "8080" }],
};

vi.mock("@/app/state/editorState", () => ({
  useEditorState: () => ({ state: { document: doc }, dispatch: () => {} }),
}));

describe("isEnvRef", () => {
  it("detects whole-value ${VAR} strings, not embedded or literal ones", () => {
    expect(isEnvRef("${PORT}")).toBe(true);
    // Embedded interpolation is ordinary text the picker can't represent.
    expect(isEnvRef("prefix-${X}")).toBe(false);
    expect(isEnvRef("8080")).toBe(false);
    expect(isEnvRef(8080)).toBe(false);
    expect(isEnvRef(true)).toBe(false);
  });
});

describe("EnvValueField", () => {
  it("lists the document's declared variables", () => {
    render(<EnvValueField value="" onChange={() => {}} />);
    expect(screen.getByRole("option", { name: "WEATHER_LAT" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "PORT" })).toBeInTheDocument();
  });

  it("stores the selection as a ${NAME} reference", () => {
    const onChange = vi.fn();
    render(<EnvValueField value="" onChange={onChange} />);
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "PORT" } });
    expect(onChange).toHaveBeenCalledWith("${PORT}");
  });

  it("surfaces a reference that is no longer declared", () => {
    render(<EnvValueField value="${GONE}" onChange={() => {}} />);
    expect(
      screen.getByRole("option", { name: "GONE (not declared)" }),
    ).toBeInTheDocument();
  });
});
