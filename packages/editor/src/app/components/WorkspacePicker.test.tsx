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

  it("lists groups under their heading, picked like the main list and searched with it", async () => {
    const select = vi.fn();
    render(
      <WorkspacePicker
        label="Project folder"
        current="orders"
        items={[{ id: "/b", name: "billing" }]}
        groups={[
          { id: "prod", label: "On prod", items: [{ id: "int:1", name: "shipping" }] },
          { id: "dev", label: "On dev", items: [], emptyLabel: "Sign in to see dev" },
        ]}
        onSelect={select}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Project folder" }));
    expect(screen.getByText("On prod")).toBeInTheDocument();
    expect(screen.getByText("Sign in to see dev")).toBeInTheDocument();

    await userEvent.type(screen.getByRole("textbox"), "ship");
    expect(screen.queryByRole("button", { name: "billing" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "shipping" }));
    expect(select).toHaveBeenCalledWith("int:1");
  });

  it("runs a row's own action without switching to it", async () => {
    const select = vi.fn();
    const push = vi.fn();
    render(
      <WorkspacePicker
        label="Project folder"
        current="orders"
        items={[{ id: "/b", name: "billing", action: { label: "Push billing", icon: <span>up</span>, onClick: push } }]}
        onSelect={select}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Project folder" }));
    await userEvent.click(screen.getByRole("button", { name: "Push billing" }));
    expect(push).toHaveBeenCalled();
    expect(select).not.toHaveBeenCalled();
  });

  it("marks the current one where it is listed, keeping its badge and action", async () => {
    const push = vi.fn();
    render(
      <WorkspacePicker
        label="Project folder"
        current="orders"
        currentId="int:1"
        items={[{ id: "/b", name: "billing" }]}
        groups={[
          {
            id: "prod",
            label: "On prod",
            items: [
              {
                id: "int:1",
                name: "Orders",
                badge: <span>in sync</span>,
                action: { label: "Push Orders", icon: <span>up</span>, onClick: push },
              },
            ],
          },
        ]}
        onSelect={() => {}}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Project folder" }));
    // Listed once, in its group, and not a button to switch to it.
    const marked = document.querySelectorAll("[aria-current]");
    expect(marked).toHaveLength(1);
    expect(marked[0]).toHaveTextContent("Orders");
    expect(marked[0]).toHaveTextContent("in sync");
    expect(screen.queryByRole("button", { name: "Orders in sync" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Push Orders" }));
    expect(push).toHaveBeenCalled();
  });
});
