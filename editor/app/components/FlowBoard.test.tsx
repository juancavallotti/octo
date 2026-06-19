import { describe, it, expect } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorStateProvider } from "@/app/state/editorState";
import DndProvider from "./DndProvider";
import Sidebar from "./Sidebar";
import Canvas from "./Canvas";

function renderEditor() {
  return render(
    <EditorStateProvider>
      <DndProvider>
        <Sidebar />
        <Canvas />
      </DndProvider>
    </EditorStateProvider>,
  );
}

function flows() {
  return screen.getAllByRole("region");
}

function stepsIn(region: HTMLElement) {
  return within(region).getByRole("list", { name: "Flow steps" });
}

describe("FlowBoard", () => {
  it("starts with no flows", () => {
    renderEditor();
    expect(screen.queryAllByRole("region")).toHaveLength(0);
    expect(
      screen.getByRole("button", { name: "Add flow" }),
    ).toBeInTheDocument();
  });

  it("auto-creates a flow when a palette item is clicked", async () => {
    renderEditor();
    await userEvent.click(screen.getByText("Log"));

    expect(flows()).toHaveLength(1);
    const items = within(stepsIn(flows()[0])).getAllByRole("listitem");
    expect(items).toHaveLength(1);
    expect(within(items[0]).getByText("Log")).toBeInTheDocument();
  });

  it("appends new flows with the Add flow button", async () => {
    renderEditor();
    await userEvent.click(screen.getByRole("button", { name: "Add flow" }));
    expect(flows()).toHaveLength(1);
    await userEvent.click(screen.getByRole("button", { name: "Add flow" }));
    expect(flows()).toHaveLength(2);
  });

  it("routes click-to-add to the most recently added (active) flow", async () => {
    renderEditor();
    await userEvent.click(screen.getByRole("button", { name: "Add flow" }));
    await userEvent.click(screen.getByRole("button", { name: "Add flow" }));
    await userEvent.click(screen.getByText("Log"));

    expect(within(stepsIn(flows()[0])).queryAllByRole("listitem")).toHaveLength(0);
    expect(within(stepsIn(flows()[1])).getAllByRole("listitem")).toHaveLength(1);
  });

  it("picks a source from the Add source dropdown", async () => {
    renderEditor();
    await userEvent.click(screen.getByRole("button", { name: "Add flow" }));
    expect(
      screen.getByRole("button", { name: "Add source" }),
    ).toBeInTheDocument();

    // Opening the dropdown reveals the available source types.
    await userEvent.click(screen.getByRole("button", { name: "Add source" }));
    const option = screen.getByRole("button", { name: /Cron schedule/ });
    await userEvent.click(option);

    // Source attached → the picker is replaced by the source node.
    expect(
      screen.queryByRole("button", { name: "Add source" }),
    ).not.toBeInTheDocument();
  });

  it("renders an added composite with its nested sub-flow slots", async () => {
    renderEditor();
    await userEvent.click(screen.getByText("If"));

    const region = flows()[0];
    expect(within(region).getByRole("list", { name: "Then" })).toBeInTheDocument();
    expect(within(region).getByRole("list", { name: "Else" })).toBeInTheDocument();
  });

  it("removes a block when its remove button is clicked", async () => {
    renderEditor();
    await userEvent.click(screen.getByText("Log"));
    expect(within(stepsIn(flows()[0])).getAllByRole("listitem")).toHaveLength(1);

    await userEvent.click(screen.getByRole("button", { name: "Remove step" }));
    expect(within(stepsIn(flows()[0])).queryAllByRole("listitem")).toHaveLength(0);
  });
});
