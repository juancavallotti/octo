import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorStateProvider, useEditorState, EditorActionType } from "../state/editorState";
import { emptyFlow, newBlock, type EditorDocument } from "../model/document";
import { EditorMetaProvider, type EditorMetaStore } from "../providers/EditorMetaProvider";
import { ConsoleProvider } from "../run/console";
import { FlowRunProvider } from "../run/FlowRunContext";
import { RunProvider } from "../run/RunContext";
import type { FlowRunRequest, RunTransport } from "../run/transport";
import FlowRunMenu from "./FlowRunMenu";

/** A runnable document: one flow with one block that needs no settings. */
function runnableDoc(): EditorDocument {
  const flow = emptyFlow("orders");
  flow.process = [newBlock("log")];
  return { flows: [flow], connectors: [], processors: [], env: [] };
}

/** A document whose flow is invalid (set-payload's `value` is required). */
function invalidDoc(): EditorDocument {
  const flow = emptyFlow("orders");
  flow.process = [newBlock("set-payload")];
  return { flows: [flow], connectors: [], processors: [], env: [] };
}

function stubTransport(onInvoke: (req: FlowRunRequest) => void): RunTransport {
  const snap = { available: true, running: false, version: null, testPath: null };
  return {
    status: async () => snap,
    start: async () => snap,
    stop: async () => {},
    sync: async () => {},
    evalCel: async () => ({ ok: true }),
    subscribeLogs: () => () => {},
    invoke: async (req) => {
      onInvoke(req);
      return { ok: true, dropped: false, timedOut: false, output: "{}", logs: [] };
    },
  };
}

/** A meta store seeded with the given file content. */
function stubMeta(content: string, onSave?: (c: string) => void): EditorMetaStore {
  return {
    load: async () => content,
    save: async (_id, c) => onSave?.(c),
    canEdit: () => true,
  };
}

function Harness({ doc }: { doc: EditorDocument }) {
  const { state, dispatch } = useEditorState();
  const flow = state.document.flows[0];
  return (
    <div>
      <button
        onClick={() =>
          dispatch({
            type: EditorActionType.LOAD_INTEGRATION,
            data: { id: "orders.yaml", name: "orders", document: doc, folderId: null },
          })
        }
      >
        load
      </button>
      {flow && <FlowRunMenu flowId={flow.id} />}
    </div>
  );
}

async function setup(opts: {
  doc?: EditorDocument;
  transport?: RunTransport;
  meta?: EditorMetaStore | null;
}) {
  const user = userEvent.setup();
  const transport = opts.transport ?? stubTransport(() => {});
  render(
    <EditorStateProvider>
      <EditorMetaProvider store={opts.meta ?? null}>
        <ConsoleProvider>
          <RunProvider transport={transport}>
            <FlowRunProvider transport={transport}>
              <Harness doc={opts.doc ?? runnableDoc()} />
            </FlowRunProvider>
          </RunProvider>
        </ConsoleProvider>
      </EditorMetaProvider>
    </EditorStateProvider>,
  );
  await user.click(screen.getByText("load"));
  return user;
}

/** The saved-input file the meta store hands back. */
const SAVED = JSON.stringify({
  version: 1,
  resources: {
    "orders.yaml": {
      flows: {
        orders: {
          inputs: [{ id: "i1", name: "happy path", data: '{"orderId":42}', vars: '{"tier":"gold"}' }],
        },
      },
    },
  },
});

describe("FlowRunMenu", () => {
  it("runs the flow with no input", async () => {
    const seen: FlowRunRequest[] = [];
    const user = await setup({ transport: stubTransport((r) => seen.push(r)) });

    await user.click(screen.getByLabelText("Run flow"));
    await user.click(screen.getByText("No input"));

    await waitFor(() => expect(seen).toHaveLength(1));
    expect(seen[0].flow).toBe("orders");
    expect(seen[0].data).toBeUndefined();
    expect(seen[0].breakAt).toBeUndefined();
  });

  it("lists the flow's saved inputs and runs with the one picked", async () => {
    const seen: FlowRunRequest[] = [];
    const user = await setup({
      transport: stubTransport((r) => seen.push(r)),
      meta: stubMeta(SAVED),
    });

    await user.click(screen.getByLabelText("Run flow"));
    await waitFor(() => expect(screen.getByText("happy path")).toBeInTheDocument());
    await user.click(screen.getByText("happy path"));

    await waitFor(() => expect(seen).toHaveLength(1));
    expect(seen[0].data).toBe('{"orderId":42}');
    // Variables ride along: a flow reading a header needs them, since no source runs.
    expect(seen[0].vars).toBe('{"tier":"gold"}');
  });

  it("adds a test input through the form", async () => {
    const saved: string[] = [];
    const user = await setup({ meta: stubMeta("", (c) => saved.push(c)) });

    await user.click(screen.getByLabelText("Run flow"));
    await user.click(screen.getByText("Add test input"));
    await user.type(screen.getByPlaceholderText("happy path"), "empty cart");
    await user.click(screen.getByRole("button", { name: "Add" }));

    // It appears in the menu immediately...
    await waitFor(() => expect(screen.getByText("empty cart")).toBeInTheDocument());
    // ...and is written down (debounced) under the flow's name.
    await waitFor(() => expect(saved.length).toBeGreaterThan(0), { timeout: 3000 });
    expect(saved[saved.length - 1]).toContain("empty cart");
  });

  it("refuses to save an input with malformed JSON", async () => {
    const user = await setup({ meta: stubMeta("") });

    await user.click(screen.getByLabelText("Run flow"));
    await user.click(screen.getByText("Add test input"));
    await user.type(screen.getByPlaceholderText("happy path"), "broken");
    // `{{` is user-event's escape for a literal brace.
    await user.type(screen.getByPlaceholderText('{ "orderId": 42 }'), "{{not json");

    expect(screen.getByText("That isn't valid JSON.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add" })).toBeDisabled();
  });

  // The runner would only reject it, so say why instead of letting them find out.
  it("blocks the run button when the document is invalid", async () => {
    await setup({ doc: invalidDoc() });

    const button = screen.getByLabelText("Run flow");
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("title", expect.stringContaining("Fix before running"));
  });

  it("warns that inputs are not kept for an unsaved draft", async () => {
    const user = await setup({
      meta: { load: async () => "", save: vi.fn(), canEdit: () => false },
    });

    await user.click(screen.getByLabelText("Run flow"));
    expect(screen.getByText("Save the flow to keep its test inputs.")).toBeInTheDocument();
  });

  it("renders nothing without a run capability", async () => {
    const user = userEvent.setup();
    render(
      <EditorStateProvider>
        <EditorMetaProvider store={null}>
          <ConsoleProvider>
            <Harness doc={runnableDoc()} />
          </ConsoleProvider>
        </EditorMetaProvider>
      </EditorStateProvider>,
    );
    await user.click(screen.getByText("load"));

    expect(screen.queryByLabelText("Run flow")).not.toBeInTheDocument();
  });
});
