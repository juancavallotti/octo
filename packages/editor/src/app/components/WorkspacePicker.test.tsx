import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import WorkspacePicker from "./WorkspacePicker";

describe("WorkspacePicker", () => {
  it("offers every action above the list, and closes on picking one", async () => {
    const open = vi.fn();
    const pull = vi.fn();
    render(
      <WorkspacePicker
        label="Project folder"
        current="orders"
        items={[]}
        onSelect={() => {}}
        actions={[
          { label: "Open folder…", onClick: open },
          { label: "From the platform…", onClick: pull },
        ]}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Project folder" }));
    expect(screen.getByRole("button", { name: "Open folder…" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "From the platform…" }));
    expect(pull).toHaveBeenCalled();
    expect(open).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Open folder…" })).not.toBeInTheDocument();
  });

  it("shows an item's badge beside its name", async () => {
    render(
      <WorkspacePicker
        label="Project folder"
        current="orders"
        items={[{ id: "/b", name: "billing", badge: <span>on prod</span> }]}
        onSelect={() => {}}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Project folder" }));
    expect(screen.getByRole("button", { name: /billing/ })).toHaveTextContent("on prod");
  });
});
