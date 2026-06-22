import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorStateProvider } from "@/app/state/editorState";
import DndProvider from "./DndProvider";
import Sidebar from "./Sidebar";
import Canvas from "./Canvas";
import SettingsPanel from "./SettingsPanel";

function renderEditor() {
  return render(
    <EditorStateProvider>
      <DndProvider>
        <Sidebar />
        <Canvas />
        <SettingsPanel />
      </DndProvider>
    </EditorStateProvider>,
  );
}

async function addFlowWithSource(name = /Cron schedule/) {
  await userEvent.click(screen.getByRole("button", { name: "Add flow" }));
  await userEvent.click(screen.getByRole("button", { name: "Add source" }));
  await userEvent.click(screen.getByRole("button", { name }));
}

describe("source flow", () => {
  it("opens the settings panel for a picked source", async () => {
    renderEditor();
    await addFlowWithSource();

    // The source node renders with its schema label...
    expect(
      screen.getByRole("button", { name: "Source: Cron schedule" }),
    ).toBeInTheDocument();
    // ...and the panel shows its fields.
    expect(screen.getByLabelText(/Schedule/)).toBeInTheDocument();
  });

  it("persists edits to a source setting", async () => {
    renderEditor();
    await addFlowWithSource();

    const schedule = screen.getByLabelText(/Schedule/);
    await userEvent.type(schedule, "@every 2s");
    expect(schedule).toHaveValue("@every 2s");
  });

  it("re-selects a source by clicking its node", async () => {
    renderEditor();
    await addFlowWithSource(/HTTP route/);

    // Close the panel, then click the node to reopen its settings.
    await userEvent.click(screen.getByRole("button", { name: "Close settings" }));
    expect(screen.queryByLabelText(/Path/)).not.toBeInTheDocument();

    await userEvent.click(
      screen.getByRole("button", { name: "Source: HTTP route" }),
    );
    expect(screen.getByLabelText(/Path/)).toBeInTheDocument();
  });

  it("removes a source from its node's corner button", async () => {
    renderEditor();
    await addFlowWithSource();
    expect(
      screen.queryByRole("button", { name: "Add source" }),
    ).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Remove source" }));
    expect(
      screen.getByRole("button", { name: "Add source" }),
    ).toBeInTheDocument();
  });
});
