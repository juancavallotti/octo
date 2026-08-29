import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { ThreadList } from "./ThreadList";
import type { MemoryThread } from "@/app/model/agentMemory";

/**
 * jsdom does no layout, so nothing here can catch an overflow by measuring one.
 * What it can hold is the rule that prevents it: the parts of a row that may be
 * cut carry the classes that let them shrink, and the parts that must survive
 * carry the class that stops them being cut instead.
 */

const THREAD: MemoryThread = {
  threadKey: "2ab89dca-fc54-44dd-a175-33187e3e7c68/98f1c2",
  title: "",
  userId: "",
  turnCount: 16,
  lastActivityAt: "2026-08-27T10:00:00Z",
};

function renderList(over: Partial<MemoryThread> = {}) {
  render(
    <ThreadList
      threads={[{ ...THREAD, ...over }]}
      selected={null}
      onOpen={vi.fn()}
      onDelete={vi.fn()}
    />,
  );
}

describe("ThreadList", () => {
  it("lets a row shrink under its own content", () => {
    // A flex item defaults to min-width:auto and refuses to shrink below what is
    // inside it, so `truncate` on a child of one never fires. An untitled thread
    // falls back to its key, and the row grew until the list ran out past the
    // edge of its own card.
    renderList();
    // Reached through the title, since the erase button's label names the
    // thread too.
    const button = screen.getByText(THREAD.threadKey).closest("button")!;
    expect(button.className).toContain("min-w-0");
  });

  it("cuts the person rather than the turn count and the date", () => {
    // Truncating the whole line would take both of those with it, and they are
    // the two things this row exists to say.
    renderList({ userId: "a-very-long-identifier-for-one-person" });

    const person = screen.getByText("a-very-long-identifier-for-one-person");
    expect(person.className).toContain("truncate");

    const meta = screen.getByText(/16 turns/);
    expect(meta.className).toContain("shrink-0");
    expect(meta.className).not.toContain("truncate");
  });

  it("still reads as one line of prose", () => {
    renderList({ userId: "clean-user", turnCount: 2 });
    expect(screen.getByText("clean-user")).toBeInTheDocument();
    expect(screen.getByText(/2 turns/)).toBeInTheDocument();
  });

  it("says a single turn in the singular", () => {
    renderList({ turnCount: 1 });
    expect(screen.getByText(/1 turn ·/)).toBeInTheDocument();
  });
});
