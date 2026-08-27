/**
 * What the agent is still carrying.
 *
 * The payload is the runtime's serialized form and this side has never parsed
 * it — that is why it is stored as opaque bytes — so what is asserted here is
 * mostly the panel's *tolerance*: it renders the shape it knows, falls back to
 * raw text for anything else, and never claims to have understood something it
 * did not.
 */

import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import { WorkingMemoryPanel } from "./WorkingMemoryPanel";

const base = {
  found: true,
  version: 3,
  iteration: 2,
  tokens: 2083,
  updatedAt: "2026-08-27T07:57:00Z",
  bytes: 9732,
  readable: true,
};

function payload(messages: unknown[]) {
  return JSON.stringify({ v: 1, tokens: 2083, messages });
}

describe("the working memory panel", () => {
  it("renders the messages the agent carries, with its counts", () => {
    render(
      <WorkingMemoryPanel
        working={{
          ...base,
          payload: payload([{ Role: "user", Text: "how do I deploy?" }]),
        }}
      />,
    );

    expect(screen.getByText("how do I deploy?")).toBeTruthy();
    expect(screen.getByText(/2,083 tokens/)).toBeTruthy();
    expect(screen.getByText(/9\.5 KB/)).toBeTruthy();
  });

  /**
   * Tool traffic is usually most of what fills a context, and a tool result
   * carries its content in a structured field with nothing in `Text` — so
   * without naming the tools these rendered as a column of blank entries.
   */
  it("names the tools on a message that carries no text of its own", () => {
    render(
      <WorkingMemoryPanel
        working={{
          ...base,
          payload: payload([
            { Role: "tool", Text: "", ToolResults: [{ Tool: "load_skill" }] },
            { Role: "assistant", Text: "reading it", ToolCalls: [{ Name: "octo_read" }] },
          ]),
        }}
      />,
    );

    expect(screen.getByText("load_skill")).toBeTruthy();
    expect(screen.getByText("octo_read")).toBeTruthy();
    expect(screen.queryByText("no text")).toBeNull();
  });

  // Anything unrecognized falls back to the raw payload rather than a partial
  // render: a viewer that guessed would show something confidently wrong about
  // what an agent remembers, and the raw text is always truthful.
  it("shows the payload verbatim when it is not the shape it knows", () => {
    render(<WorkingMemoryPanel working={{ ...base, payload: "not json at all" }} />);

    expect(screen.getByText("not json at all")).toBeTruthy();
  });

  it("describes a payload that is not text rather than mangling it", () => {
    render(<WorkingMemoryPanel working={{ ...base, readable: false }} />);

    expect(screen.getByText(/not text/)).toBeTruthy();
  });

  // Ordinary, not an error: a conversation that ended cleanly keeps its
  // transcript and has nothing to resume from.
  it("says plainly when there is no live context", () => {
    render(
      <WorkingMemoryPanel
        working={{ ...base, found: false, tokens: 0, bytes: 0, readable: false }}
      />,
    );

    expect(screen.getByText(/no live context/)).toBeTruthy();
  });
});
