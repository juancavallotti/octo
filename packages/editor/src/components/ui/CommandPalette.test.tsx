import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import CommandPalette from "./CommandPalette";

/**
 * The primitive is controlled: the parent owns the query and the highlighted index,
 * and the list it is handed changes underneath both. What is tested here is the part
 * of that contract the parent cannot uphold on its own.
 */

const ITEMS = ["alpha", "beta", "gamma"];

function renderPalette(props: Partial<React.ComponentProps<typeof CommandPalette<string>>> = {}) {
  const onPick = vi.fn();
  const onClose = vi.fn();
  render(
    <CommandPalette
      open
      onClose={onClose}
      query=""
      onQueryChange={() => {}}
      items={ITEMS}
      active={0}
      onActiveChange={() => {}}
      onPick={onPick}
      keyOf={(i) => i}
      renderItem={(item) => <span>{item}</span>}
      label="Pick one"
      {...props}
    />,
  );
  return { onPick, onClose };
}

describe("CommandPalette", () => {
  it("focuses its input, so the keystroke that summoned it is not wasted", () => {
    renderPalette();
    expect(screen.getByRole("combobox")).toHaveFocus();
  });

  it("holds focus against Tab, which aria-modal alone does not do", async () => {
    renderPalette();
    await userEvent.tab();
    // The rows are options rather than buttons, so the box is the only stop inside —
    // and Tab must not walk out to the page the dialog claims to have made inert.
    expect(screen.getByRole("combobox")).toHaveFocus();
    await userEvent.tab({ shift: true });
    expect(screen.getByRole("combobox")).toHaveFocus();
  });

  it("hands focus back to where it came from when it closes", () => {
    const summoner = document.createElement("button");
    document.body.append(summoner);
    summoner.focus();

    const { unmount } = render(
      <CommandPalette
        open
        onClose={() => {}}
        query=""
        onQueryChange={() => {}}
        items={ITEMS}
        active={0}
        onActiveChange={() => {}}
        onPick={() => {}}
        keyOf={(i) => i}
        renderItem={(item) => <span>{item}</span>}
        label="Pick one"
      />,
    );
    expect(screen.getByRole("combobox")).toHaveFocus();

    unmount();
    // Otherwise the page is left focused on <body>, where no shortcut reaches.
    expect(summoner).toHaveFocus();
    summoner.remove();
  });
});
