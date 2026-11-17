import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { EditorDocument } from "../../model/document";
import { setCapabilities, type Capabilities } from "../../schema";
import ProviderToolListEditor from "./ProviderToolListEditor";

const caps: Capabilities = {
  blocks: [],
  connectors: [
    {
      type: "llm-anthropic",
      label: "Anthropic",
      category: "llm",
      settings: [],
      sources: [],
      providerTools: [
        {
          type: "web_search",
          label: "Web search",
          description: "Searches the web.",
          fields: [{ name: "maxUses", label: "Max uses", type: "number", required: false }],
        },
        { type: "code_execution", label: "Code execution", fields: [] },
      ],
    },
    { type: "llm-plain", label: "Plain", category: "llm", settings: [], sources: [] },
  ],
};

const doc: EditorDocument = {
  connectors: [
    { id: "c1", name: "claude", type: "llm-anthropic", settings: {} },
    { id: "c2", name: "plain", type: "llm-plain", settings: {} },
  ],
  flows: [],
  processors: [],
  env: [],
};

vi.mock("../../state/useProject", () => ({
  useProjectView: () => doc,
}));

beforeEach(() => setCapabilities(caps));

describe("ProviderToolListEditor", () => {
  it("asks for a connection before offering anything", () => {
    render(<ProviderToolListEditor value={undefined} connection={undefined} onChange={() => {}} />);
    expect(screen.getByText(/Select an LLM connection/)).toBeInTheDocument();
  });

  it("adds a tool from the connection's catalogue and edits its settings", async () => {
    const onChange = vi.fn();
    render(<ProviderToolListEditor value={undefined} connection="claude" onChange={onChange} />);

    await userEvent.selectOptions(screen.getByLabelText("Add provider tool"), "web_search");
    expect(onChange).toHaveBeenLastCalledWith([{ type: "web_search" }]);
    expect(screen.getByText("Searches the web.")).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText("Max uses"), "3");
    expect(onChange).toHaveBeenLastCalledWith([{ type: "web_search", settings: { maxUses: 3 } }]);

    // An enabled tool is no longer offered.
    expect(screen.queryByRole("option", { name: "Web search" })).toBeNull();
  });

  it("removes a tool, clearing the setting when none are left", async () => {
    const onChange = vi.fn();
    render(
      <ProviderToolListEditor value={[{ type: "code_execution" }]} connection="claude" onChange={onChange} />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Remove Code execution" }));
    expect(onChange).toHaveBeenLastCalledWith(undefined);
  });

  it("keeps and flags a stored tool the connection does not offer", () => {
    render(
      <ProviderToolListEditor value={[{ type: "web_search" }]} connection="plain" onChange={() => {}} />,
    );
    expect(screen.getByText("web_search")).toBeInTheDocument();
    expect(screen.getByText("not offered by this connection")).toBeInTheDocument();
    expect(screen.getByText("This connection runs no provider tools.")).toBeInTheDocument();
  });
});
