import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import SaveButton from "./SaveButton";
import { EditorStateProvider } from "../state/editorState";
import {
  FileSystemProvider,
  type FileSystemCapability,
} from "../providers/FileSystemProvider";
import { SaveProvider } from "../save/SaveContext";

// A fake capability injected via context — no module mocking needed, which is
// the point of the FileSystemProvider seam.
const fakeFs: FileSystemCapability = {
  load: async () => ({ id: "x", name: "n", definition: "" }),
  save: async () => ({ id: "x", name: "n", definition: "" }),
};

function renderWith(value: FileSystemCapability | null) {
  return render(
    <EditorStateProvider>
      <FileSystemProvider value={value}>
        <SaveProvider>
          <SaveButton />
        </SaveProvider>
      </FileSystemProvider>
    </EditorStateProvider>,
  );
}

describe("SaveButton", () => {
  it("renders nothing without a filesystem capability", () => {
    renderWith(null);
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
  });

  it("renders the Save control when a filesystem capability is provided", () => {
    renderWith(fakeFs);
    expect(
      screen.getByRole("button", { name: /save/i }),
    ).toBeInTheDocument();
  });

  // A backend that declines writes still loads: the control stays, disabled,
  // carrying that backend's own sentence — rather than vanishing and leaving a
  // header that reads as broken.
  it("disables the control and shows why when the backend declines writes", () => {
    renderWith({ ...fakeFs, readOnly: "Needs the Developer or Operator role" });
    const button = screen.getByRole("button", { name: /save/i });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("title", "Needs the Developer or Operator role");
  });
});
