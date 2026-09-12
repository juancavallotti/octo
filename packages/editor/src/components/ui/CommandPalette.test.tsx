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

  it("picks the highlighted row on Enter", async () => {
    const { onPick } = renderPalette({ active: 1 });
    await userEvent.keyboard("{Enter}");
    expect(onPick).toHaveBeenCalledWith("beta");
  });

  it("still has a selection when the index points past the list", async () => {
    // The parent's index goes stale the moment the list it indexes into shrinks —
    // a filter narrowing under a highlight the pointer put there. Left unclamped,
    // nothing is highlighted and Enter picks nothing, which reads as a palette that
    // has stopped working. The parent cannot prevent this; only the list's owner
    // knows how long it is.
    const { onPick } = renderPalette({ active: 7 });
    expect(screen.getByRole("option", { name: "gamma" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await userEvent.keyboard("{Enter}");
    expect(onPick).toHaveBeenCalledWith("gamma");
  });

  it("survives a negative index the same way", () => {
    renderPalette({ active: -3 });
    expect(screen.getByRole("option", { name: "alpha" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("picks nothing when there is nothing to pick", async () => {
    const { onPick } = renderPalette({ items: [], empty: <p>No matches</p> });
    await userEvent.keyboard("{Enter}");
    expect(onPick).not.toHaveBeenCalled();
    expect(screen.getByText("No matches")).toBeInTheDocument();
  });

  it("closes on Escape and on a click outside, but not on one inside", async () => {
    const { onClose } = renderPalette();
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);

    await userEvent.click(screen.getByRole("dialog"));
    expect(onClose).toHaveBeenCalledTimes(1);

    // The backdrop is the dialog's parent, and dismissal is bound to mousedown so a
    // drag that ends outside the panel is not a dismissal.
    const backdrop = screen.getByRole("dialog").parentElement as HTMLElement;
    await userEvent.click(backdrop);
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("wraps the highlight around both ends", async () => {
    const onActiveChange = vi.fn();
    renderPalette({ active: 0, onActiveChange });
    await userEvent.keyboard("{ArrowUp}");
    expect(onActiveChange).toHaveBeenCalledWith(ITEMS.length - 1);

    onActiveChange.mockClear();
    await userEvent.keyboard("{ArrowDown}");
    expect(onActiveChange).toHaveBeenCalledWith(1);
  });
});
