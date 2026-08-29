import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { AppPicker } from "./AppPicker";

interface App {
  id: string;
  name: string;
}

const APPS: App[] = [
  { id: "1", name: "orders" },
  { id: "2", name: "billing" },
  { id: "3", name: "inventory" },
];

function renderPicker(overrides: Partial<Parameters<typeof AppPicker<App>>[0]> = {}) {
  const onSelect = vi.fn();
  render(
    <AppPicker<App>
      items={APPS}
      selected={null}
      onSelect={onSelect}
      toKey={(a) => a.id}
      toText={(a) => a.name}
      renderRow={(a) => a.name}
      label="Application"
      {...overrides}
    />,
  );
  return { onSelect };
}

describe("AppPicker", () => {
  it("shows the placeholder until something is chosen", () => {
    renderPicker();
    expect(screen.getByRole("button", { name: "Application" })).toHaveTextContent(
      "Search…",
    );
  });

  it("shows the chosen item on the trigger", () => {
    renderPicker({ selected: APPS[0] });
    expect(screen.getByRole("button", { name: "Application" })).toHaveTextContent(
      "orders",
    );
  });

  it("opens to the whole list", async () => {
    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByRole("button", { name: "Application" }));

    expect(screen.getAllByRole("option")).toHaveLength(3);
  });

  it("finds an app the query has a typo in", async () => {
    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByRole("button", { name: "Application" }));
    await user.type(screen.getByRole("combobox"), "bling");

    // Weak matches are kept, at the bottom — the point of ranking over filtering
    // is that a typo demotes rather than disappears.
    expect(screen.getAllByRole("option")[0]).toHaveTextContent("billing");
  });

  it("ranks the closest match first rather than the first one listed", async () => {
    const user = userEvent.setup();
    renderPicker({
      items: [
        { id: "1", name: "orders-reconciliation-worker" },
        { id: "2", name: "orders" },
      ],
    });

    await user.click(screen.getByRole("button", { name: "Application" }));
    await user.type(screen.getByRole("combobox"), "ord");

    expect(screen.getAllByRole("option")[0]).toHaveTextContent(/^orders$/);
  });

  it("picks with the keyboard alone", async () => {
    const user = userEvent.setup();
    const { onSelect } = renderPicker();

    await user.click(screen.getByRole("button", { name: "Application" }));
    await user.keyboard("{ArrowDown}{Enter}");

    expect(onSelect).toHaveBeenCalledWith(APPS[1]);
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("picks with the pointer", async () => {
    const user = userEvent.setup();
    const { onSelect } = renderPicker();

    await user.click(screen.getByRole("button", { name: "Application" }));
    await user.click(screen.getByRole("option", { name: "inventory" }));

    expect(onSelect).toHaveBeenCalledWith(APPS[2]);
  });

  it("closes on Escape without choosing", async () => {
    const user = userEvent.setup();
    const { onSelect } = renderPicker();

    await user.click(screen.getByRole("button", { name: "Application" }));
    await user.keyboard("{Escape}");

    expect(onSelect).not.toHaveBeenCalled();
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(screen.getByRole("button", { name: "Application" })).toHaveFocus();
  });

  it("says nothing matches rather than looking empty", async () => {
    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByRole("button", { name: "Application" }));
    await user.type(screen.getByRole("combobox"), "zzzz");

    expect(screen.getByText("Nothing matches what you typed.")).toBeInTheDocument();
  });

  it("distinguishes an empty source from an empty result", async () => {
    const user = userEvent.setup();
    renderPicker({ items: [], empty: "No app has published a trace." });

    await user.click(screen.getByRole("button", { name: "Application" }));

    expect(screen.getByText("No app has published a trace.")).toBeInTheDocument();
  });

  it("starts a fresh search each time it opens", async () => {
    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByRole("button", { name: "Application" }));
    await user.type(screen.getByRole("combobox"), "billing");
    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "Application" }));

    expect(screen.getByRole("combobox")).toHaveValue("");
    expect(screen.getAllByRole("option")).toHaveLength(3);
  });

  it("renders the accessory beside the trigger, not behind the selection", () => {
    renderPicker({ accessory: <span>namespace</span> });
    expect(screen.getByText("namespace")).toBeInTheDocument();
  });
});
