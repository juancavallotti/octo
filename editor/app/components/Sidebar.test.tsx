import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorStateProvider } from "@/app/state/editorState";
import DndProvider from "./DndProvider";
import Sidebar from "./Sidebar";

function renderSidebar() {
  return render(
    <EditorStateProvider>
      <DndProvider>
        <Sidebar />
      </DndProvider>
    </EditorStateProvider>,
  );
}

describe("Sidebar", () => {
  it("lists building blocks from the capability schema", () => {
    renderSidebar();
    expect(screen.getByText("Set Payload")).toBeInTheDocument();
    expect(screen.getByText("SQL")).toBeInTheDocument();
  });

  it("filters components by query (local useState)", async () => {
    renderSidebar();
    expect(screen.getByText("SQL")).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText("Filter components"), "log");

    expect(screen.queryByText("SQL")).not.toBeInTheDocument();
    expect(screen.getByText("Log")).toBeInTheDocument();
  });
});
