import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import AgentMessage from "./AgentMessage";
import type { Turn } from "./useAgentChat";

function agentTurn(over: Partial<Turn> = {}): Turn {
  return { id: "t1", role: "agent", text: "", tools: [], streaming: false, ...over };
}

describe("AgentMessage", () => {
  it("renders a user turn as plain text, not as markdown", () => {
    render(
      <AgentMessage
        turn={{ id: "u1", role: "user", text: "**not bold**", tools: [], streaming: false }}
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
        turn={agentTurn({
          tools: [
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

  it("shows a note for a guardrail or an error", () => {
    render(<AgentMessage turn={agentTurn({ note: "Outside my remit." })} />);

    expect(screen.getByText("Outside my remit.")).toBeTruthy();
  });
});

/** An agent turn carrying the given markdown. */
function agentMessage(text: string) {
  return <AgentMessage turn={agentTurn({ text })} />;
}
