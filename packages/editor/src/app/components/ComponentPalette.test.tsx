import { beforeEach, describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { setCapabilities } from "../schema";
import { EditorActionType, EditorStateProvider, useEditorState } from "../state/editorState";
import type { BlockSpec } from "../schema/types";
import ComponentPalette from "./ComponentPalette";

const block = (type: string, label: string, group: string): BlockSpec => ({
  type,
  label,
  group,
  category: "processor",
  icon: "box",
  description: label,
  fields: [],
});

/**
 * The palette is driven entirely from the keyboard in these tests, because that is
 * the only way it is meant to be used: a mouse user has the sidebar.
 */
beforeEach(() => {
  setCapabilities({
    blocks: [
      block("log", "Log", "Core"),
      block("rest", "REST Call", "Integration"),
      {
        ...block("switch", "Switch", "Flow"),
        fields: [
          { name: "cases", type: "case-list", label: "Cases", required: false },
        ],
      },
    ],
    connectors: [],
  });
});

function Harness() {
  const { state, dispatch } = useEditorState();
  const flow = state.document.flows[0];
  const sub = flow?.process[0]?.slots?.cases?.[0];
  return (
    <div>
      <ComponentPalette />
      <button onClick={() => dispatch({ type: EditorActionType.ADD_BLOCK, data: { blockType: "switch" } })}>
        add-switch
      </button>
      <button
        onClick={() =>
          dispatch({ type: EditorActionType.ADD_BLOCK, data: { blockType: "log", flowId: sub?.id } })
        }
      >
        seed-branch
      </button>
      <p data-testid="top">{flow ? flow.process.map((b) => b.type).join(",") : ""}</p>
      <p data-testid="branch">{sub ? sub.process.map((b) => b.type).join(",") : "no-branch"}</p>
    </div>
  );
}

const renderHarness = () =>
  render(
    <EditorStateProvider>
      <Harness />
    </EditorStateProvider>,
  );

const open = (user: ReturnType<typeof userEvent.setup>) => user.keyboard("{Meta>}/{/Meta}");
const top = () => screen.getByTestId("top").textContent;

describe("ComponentPalette", () => {
  it("opens on Cmd+/ and lists the schema's blocks", async () => {
    const user = userEvent.setup();
    renderHarness();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    await open(user);
    expect(screen.getByRole("dialog", { name: "Add a component" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /Log/ })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /REST Call/ })).toBeInTheDocument();
  });

  it("filters as you type and inserts the first match on Enter", async () => {
    const user = userEvent.setup();
    renderHarness();
    await open(user);
    await user.keyboard("rest");
    await user.keyboard("{Enter}");

    expect(top()).toBe("rest");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("walks the list with the arrow keys", async () => {
    const user = userEvent.setup();
    renderHarness();
    await open(user);
    await user.keyboard("{ArrowDown}{Enter}");
    // Second row of the unfiltered list, which is schema order.
    expect(top()).toBe("rest");
  });

  it("closes on Escape without adding anything", async () => {
    const user = userEvent.setup();
    renderHarness();
    await open(user);
    await user.keyboard("{Escape}");

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(top()).toBe("");
  });

  it("says so when nothing matches", async () => {
    const user = userEvent.setup();
    renderHarness();
    await open(user);
    await user.keyboard("zzzz");
    expect(screen.getByText("No components match.")).toBeInTheDocument();
  });

  it("appends after the selected block", async () => {
    const user = userEvent.setup();
    renderHarness();
    await open(user);
    await user.keyboard("log{Enter}");
    await open(user);
    await user.keyboard("rest{Enter}");
    expect(top()).toBe("log,rest");
  });

  it("inserts into the composite branch the selected block is in", async () => {
    const user = userEvent.setup();
    renderHarness();
    await user.click(screen.getByText("add-switch"));
    // Drop a block into the switch's first case the way a drag would, which also
    // leaves it selected.
    await user.click(screen.getByText("seed-branch"));

    await open(user);
    await user.keyboard("rest{Enter}");

    // The second block must land beside the first, inside the branch — not back out
    // on the top-level flow, which is all activeFlowId could have told us.
    expect(screen.getByTestId("branch")).toHaveTextContent("log,rest");
    expect(top()).toBe("switch");
  });
});
