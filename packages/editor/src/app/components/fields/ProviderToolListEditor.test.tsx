import { describe, it, expect, vi, beforeEach } from "vitest";
import { useState } from "react";
import { act, render, screen } from "@testing-library/react";
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
        {
          type: "web_fetch",
          label: "Web fetch",
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

/**
 * A parent that owns the value, as the settings panel does: every change is fed
 * back, and `setValue` lets a test change it from outside, as an undo would.
 */
let setValue: (v: unknown) => void = () => {};
function Controlled({ initial, onChange }: { initial?: unknown; onChange?: (v: unknown) => void }) {
  const [value, set] = useState<unknown>(initial);
  setValue = set;
  return (
    <ProviderToolListEditor
      value={value}
      connection="claude"
      onChange={(v) => {
        set(v);
        onChange?.(v);
      }}
    />
  );
}

describe("ProviderToolListEditor", () => {
  it("asks for a connection before offering anything", () => {
    render(<ProviderToolListEditor value={undefined} connection={undefined} onChange={() => {}} />);
    expect(screen.getByText(/Select an LLM connection/)).toBeInTheDocument();
  });

  it("adds a tool from the connection's catalogue and edits its settings", async () => {
    const onChange = vi.fn();
    render(<Controlled onChange={onChange} />);

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
    render(<Controlled initial={[{ type: "code_execution" }]} onChange={onChange} />);
    await userEvent.click(screen.getByRole("button", { name: "Remove Code execution" }));
    expect(onChange).toHaveBeenLastCalledWith(undefined);
  });

  it("follows an outside change, so a cleared list does not come back on the next add", async () => {
    const onChange = vi.fn();
    render(<Controlled initial={[{ type: "web_search" }]} onChange={onChange} />);
    expect(screen.getByText("Web search")).toBeInTheDocument();

    act(() => setValue(undefined));
    expect(screen.queryByText("Web search")).toBeNull();

    await userEvent.selectOptions(screen.getByLabelText("Add provider tool"), "code_execution");
    expect(onChange).toHaveBeenLastCalledWith([{ type: "code_execution" }]);
  });

  it("gives the same setting on two tools its own control", async () => {
    const onChange = vi.fn();
    render(<Controlled initial={[{ type: "web_search" }, { type: "web_fetch" }]} onChange={onChange} />);

    const [search, fetch] = screen.getAllByLabelText("Max uses");
    expect(search).not.toBe(fetch);
    await userEvent.type(fetch, "2");
    expect(onChange).toHaveBeenLastCalledWith([
      { type: "web_search" },
      { type: "web_fetch", settings: { maxUses: 2 } },
    ]);
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
