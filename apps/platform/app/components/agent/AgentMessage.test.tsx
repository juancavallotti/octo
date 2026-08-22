import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import AgentMessage from "./AgentMessage";
import { newTurn } from "./turns";
import type { Segment, Turn } from "./turns";

function agentTurn(over: Partial<Turn> = {}): Turn {
  return { ...newTurn("t1", "agent"), ...over };
}

/** A turn made of these segments, in this order. */
function withSegments(...segments: Segment[]): Turn {
  return agentTurn({ segments });
}

describe("AgentMessage", () => {
  it("renders a user turn as plain text, not as markdown", () => {
    render(
      <AgentMessage
        turn={newTurn("u1", "user", "**not bold**")}
      />,
    );

    expect(screen.getByText("**not bold**")).toBeTruthy();
  });

  it("renders the agent's markdown: headings, lists, tables and code", () => {
    const { container } = render(
      agentMessage(
        [
          "## Findings",
          "",
          "- first",
          "- second",
          "",
          "| Name | State |",
          "| --- | --- |",
          "| api | running |",
          "",
          "```yaml",
          "type: rest",
          "```",
        ].join("\n"),
      ),
    );

    expect(screen.getByRole("heading", { name: "Findings" })).toBeTruthy();
    expect(container.querySelectorAll("li")).toHaveLength(2);
    // Tables come from remark-gfm, so this also pins that the plugin is wired up.
    expect(container.querySelector("table")).toBeTruthy();
    expect(screen.getByText("running")).toBeTruthy();
    expect(container.querySelector("pre code")?.className).toContain("language-yaml");
  });

  // The answer is generated from text other people wrote — integration definitions,
  // resource contents, pod logs — so a model repeating a script tag back must render
  // it as characters. react-markdown builds elements rather than HTML, and this is
  // the test that says so out loud.
  it("escapes html in the answer instead of executing it", () => {
    const { container } = render(agentMessage('<script>alert(1)</script> and <img src=x onerror=y>'));

    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("img")).toBeNull();
    expect(container.textContent).toContain("<script>alert(1)</script>");
  });

  it("opens links in a new tab without leaking the referrer", () => {
    const { container } = render(agentMessage("[docs](https://example.com)"));
    const link = container.querySelector("a")!;

    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toContain("noreferrer");
  });

  it("shows a chip per tool, spinning until its result lands", () => {
    const { container } = render(
      <AgentMessage
        turn={withSegments({
          kind: "tools",
          iter: 1,
          runs: [
            { id: "c1", tool: "octo_api", done: true, failed: false },
            { id: "c2", tool: "read_api_docs", done: false, failed: false },
          ],
        })}
      />,
    );

    expect(screen.getByText("octo_api")).toBeTruthy();
    expect(screen.getByText("read_api_docs")).toBeTruthy();
    expect(container.querySelectorAll(".animate-spin")).toHaveLength(1);
  });

  // The order is the story: he thought, looked something up, thought again about
  // what he found, and then answered. Flattened into one block of reasoning and
  // one list of tools — which is what this used to render — none of that survives.
  it("renders segments in the order they happened", () => {
    const { container } = render(
      <AgentMessage
        turn={withSegments(
          { kind: "thinking", iter: 1, text: "first I should look" },
          { kind: "tools", iter: 1, runs: [{ id: "c1", tool: "octo_api", done: true, failed: false }] },
          { kind: "thinking", iter: 2, text: "now I know" },
          { kind: "text", iter: 2, text: "There are three." },
        )}
      />,
    );

    // Read off the collapsed labels rather than the reasoning itself: only the
    // last stretch stays open, so the earlier ones are present as headings.
    const text = container.textContent ?? "";
    const firstThought = text.indexOf("Thought about it");
    const tool = text.indexOf("octo_api");
    const secondThought = text.indexOf("Thought about it", tool);
    const answer = text.indexOf("There are three.");

    expect(firstThought).toBeGreaterThanOrEqual(0);
    expect(firstThought).toBeLessThan(tool);
    expect(tool).toBeLessThan(secondThought);
    expect(secondThought).toBeLessThan(answer);
  });

  // Reasoning is the main event only while nothing has followed it, which is
  // exactly what being the last segment means — so the first panel is closed and
  // the second, with nothing after it, is open.
  it("leaves only the last stretch of reasoning open", () => {
    render(
      <AgentMessage
        turn={withSegments(
          { kind: "thinking", iter: 1, text: "the early thought" },
          { kind: "text", iter: 1, text: "an answer" },
          { kind: "thinking", iter: 2, text: "the late thought" },
        )}
      />,
    );

    const panels = screen.getAllByRole("button", { expanded: false });
    expect(panels).toHaveLength(1);
    expect(screen.getByText("the late thought")).toBeTruthy();
    expect(screen.queryByText("the early thought")).toBeNull();
  });

  it("says when the conversation was shortened, and by how much", () => {
    render(<AgentMessage turn={withSegments({ kind: "compaction", iter: 3, strategy: "summarize", done: true, dropped: 12 })} />);

    expect(screen.getByText(/Shortened the conversation/)).toBeTruthy();
    expect(screen.getByText(/12 earlier messages/)).toBeTruthy();
  });

  // A message accepted and never answered is the one case where something a person
  // sent goes nowhere, so it must not be silent.
  it("says when a message he took was never answered", () => {
    render(
      <AgentMessage
        turn={withSegments({ kind: "signal", iter: 8, signal: "unanswered", text: "and the logs?" })}
      />,
    );

    expect(screen.getByText(/ran out of steps before answering/)).toBeTruthy();
  });

  it("shows a note for a guardrail or an error", () => {
    render(<AgentMessage turn={agentTurn({ note: "Outside my remit." })} />);

    expect(screen.getByText("Outside my remit.")).toBeTruthy();
  });
});

/** An agent turn carrying the given markdown. */
function agentMessage(text: string) {
  return <AgentMessage turn={withSegments({ kind: "text", iter: 1, text })} />;
}
