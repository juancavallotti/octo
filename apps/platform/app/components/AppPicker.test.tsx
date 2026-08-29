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

  it("says it is loading rather than repeating the caller's empty message", async () => {
    // An empty list mid-fetch is not the empty list that message describes, and
    // those messages tend to name a cause that would be a guess this early.
    const user = userEvent.setup();
    renderPicker({ items: [], loading: true, empty: "Tracing is off by default." });

    await user.click(screen.getByRole("button", { name: "Application" }));

    expect(screen.getByText("Loading…")).toBeInTheDocument();
    expect(screen.queryByText("Tracing is off by default.")).toBeNull();
  });

  it("points at the active option with an id an assistive reader can resolve", async () => {
    // aria-activedescendant holds one ID reference, and an ID reference cannot
    // contain whitespace — which a caller's key is free to.
    const user = userEvent.setup();
    renderPicker({ toKey: (a: App) => `${a.id} spaced` });

    await user.click(screen.getByRole("button", { name: "Application" }));
    const target = screen.getByRole("combobox").getAttribute("aria-activedescendant");

    expect(target).toBeTruthy();
    expect(target).not.toMatch(/\s/);
    expect(document.getElementById(target!)).toBe(screen.getAllByRole("option")[0]);
  });

  it("hands focus back to the trigger when Tab closes it", async () => {
    // Closing unmounts the search field in the same commit, so moving focus
    // onward from it would drop it to the body and restart the next Tab at the
    // top of the page.
    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByRole("button", { name: "Application" }));
    await user.tab();

    expect(screen.queryByRole("listbox")).toBeNull();
    expect(screen.getByRole("button", { name: "Application" })).toHaveFocus();
  });

  it("renders the accessory beside the trigger, not behind the selection", () => {
    renderPicker({ accessory: <span>namespace</span> });
    expect(screen.getByText("namespace")).toBeInTheDocument();
  });
});
