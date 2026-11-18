import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorStateProvider } from "../state/editorState";
import ConnectionsLauncher from "./ConnectionsLauncher";

function renderLauncher() {
  return render(
    <EditorStateProvider>
      <ConnectionsLauncher />
    </EditorStateProvider>,
  );
}

async function openAddMenu() {
  await userEvent.click(screen.getByRole("button", { name: "Connections" }));
  await userEvent.click(screen.getByRole("button", { name: /Add connection/ }));
  return screen.getByLabelText("Filter connector types");
}

describe("ConnectionsLauncher", () => {
  it("opens the connector types with the filter focused", async () => {
    renderLauncher();
    const filter = await openAddMenu();
    expect(filter).toHaveFocus();
    expect(screen.getByText("Cron")).toBeInTheDocument();
    expect(screen.getByText("Slack")).toBeInTheDocument();
  });

  it("filters by label and by type id", async () => {
    renderLauncher();
    const filter = await openAddMenu();

    await userEvent.type(filter, "llm");
    expect(screen.getByText("Anthropic")).toBeInTheDocument();
    expect(screen.getByText("Gemini")).toBeInTheDocument();
    expect(screen.queryByText("Cron")).not.toBeInTheDocument();

    await userEvent.clear(filter);
    await userEvent.type(filter, "queue");
    expect(screen.getByText("Platform Queue")).toBeInTheDocument();
    expect(screen.queryByText("Slack")).not.toBeInTheDocument();
  });

  it("says so when nothing matches", async () => {
    renderLauncher();
    await userEvent.type(await openAddMenu(), "zzz");
    expect(screen.getByText("No connector matches.")).toBeInTheDocument();
  });

  it("adds the first match on Enter", async () => {
    renderLauncher();
    await userEvent.type(await openAddMenu(), "cron{Enter}");

    // The popover closed; reopening lists the new connection.
    expect(screen.queryByLabelText("Filter connector types")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Connections" }));
    expect(screen.queryByText("No connections yet.")).not.toBeInTheDocument();
    expect(screen.getByText("Cron")).toBeInTheDocument();
  });

  it("reopens with the filter cleared", async () => {
    renderLauncher();
    await userEvent.type(await openAddMenu(), "slack");
    await userEvent.keyboard("{Escape}");

    const filter = await openAddMenu();
    expect(filter).toHaveValue("");
    expect(screen.getByText("Cron")).toBeInTheDocument();
  });
});
