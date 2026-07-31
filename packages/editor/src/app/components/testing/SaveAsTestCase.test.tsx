import { describe, it, expect } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  EditorStateProvider,
  useEditorState,
  EditorActionType,
} from "../../state/editorState";
import { emptyFlow, newBlock, type EditorDocument } from "../../model/document";
import { EditorMetaProvider, type EditorMetaStore } from "../../providers/EditorMetaProvider";
import {
  TestSuiteProvider,
  type TestSuiteStore,
} from "../../providers/TestSuiteProvider";
import { ConsoleProvider } from "../../run/console";
import { FlowRunProvider, useFlowRun } from "../../run/FlowRunContext";
import { RunProvider } from "../../run/RunContext";
import { emptyTotals, type RunTransport } from "../../run/transport";
import FlowRunMenu from "../FlowRunMenu";
import ResultsTab from "../console/ResultsTab";

/**
 * The second bridge: a run in the console becomes a committed test case.
 *
 * The assertions here are mostly about what is NOT written. A promoted case that asserts
 * the whole result envelope, or every variable the run happened to build up, is a test
 * that fails on its next honest run — and one that fails for a reason having nothing to
 * do with the flow is worse than no test at all.
 */

function seedDoc(): EditorDocument {
  const flow = emptyFlow("orders");
  const block = newBlock("log");
  block.name = "audit";
  flow.process = [block];
  return { flows: [flow], connectors: [], processors: [], env: [] };
}

/** The message `octo invoke` prints: an envelope, of which only `body` is the answer. */
const MESSAGE = '{"event_id":"e1","variables":{"tier":"gold"},"body":{"greeting":"hi"}}';

/** A saved input, so the promoted case has something to carry as its own. */
const SAVED = JSON.stringify({
  version: 1,
  resources: {
    "orders.yaml": {
      flows: {
        orders: { inputs: [{ id: "i1", name: "happy path", data: '{"orderId":42}' }] },
      },
    },
  },
});

function transportOf(output = MESSAGE, failed = false): RunTransport {
  const snap = {
    available: true,
    running: false,
    version: null,
    testAvailable: true,
    testVersion: null,
    testPath: null,
  };
  return {
    status: async () => snap,
    start: async () => snap,
    stop: async () => {},
    sync: async () => {},
    evalCel: async () => ({ ok: true }),
    subscribeLogs: () => () => {},
    test: async () => ({
      ok: false,
      timedOut: false,
      totals: emptyTotals(),
      suites: [],
      logs: [],
      error: "not what this exercises",
    }),
    invoke: async (req) => ({
      ok: !failed,
      dropped: false,
      timedOut: false,
      output: req.breakAt ? "" : output,
      // What the console really has for a failed flow: the runner's stderr, timestamp
      // and all — not the flow's own error message.
      logs: failed
        ? ['time=2026-07-30T12:00:00Z level=ERROR msg="cli stopped with error" error="flow \\"orders\\" failed: card declined"']
        : [],
      ...(req.breakAt
        ? { breakpoint: { reached: true, block: req.breakAt, message: { body: {} } } }
        : {}),
    }),
  };
}

function metaStore(content: string): EditorMetaStore {
  return { load: async () => content, save: async () => {}, canEdit: () => true };
}

function Harness() {
  const { state, dispatch } = useEditorState();
  const flowRun = useFlowRun();
  const flow = state.document.flows[0];
  const block = flow?.process[0];
  return (
    <div>
      <button
        onClick={() =>
          dispatch({
            type: EditorActionType.LOAD_INTEGRATION,
            data: { id: "orders.yaml", name: "orders", document: seedDoc(), folderId: null },
          })
        }
      >
        load
      </button>
      {flow && <FlowRunMenu flowId={flow.id} />}
      <button onClick={() => block && flowRun?.runToBlock(block.id)}>run to block</button>
      <ResultsTab results={flowRun?.results ?? []} />
    </div>
  );
}

async function setup(
  opts: {
    suites?: { flow: string; content: string }[];
    canEdit?: boolean;
    mounted?: boolean;
    output?: string;
    failed?: boolean;
  } = {},
) {
  const user = userEvent.setup();
  const written: { flow: string; content: string }[] = [];
  const store: TestSuiteStore = {
    list: async () => opts.suites ?? [],
    save: async (_id, flow, content) => void written.push({ flow, content }),
    remove: async () => {},
    canEdit: () => opts.canEdit ?? true,
  };
  const transport = transportOf(opts.output, opts.failed);
  const tree = (
    <EditorStateProvider>
      <EditorMetaProvider store={metaStore(SAVED)}>
        <ConsoleProvider>
          <RunProvider transport={transport}>
            <FlowRunProvider transport={transport}>
              {opts.mounted === false ? (
                <Harness />
              ) : (
                <TestSuiteProvider store={store}>
                  <Harness />
                </TestSuiteProvider>
              )}
            </FlowRunProvider>
          </RunProvider>
        </ConsoleProvider>
      </EditorMetaProvider>
    </EditorStateProvider>
  );
  render(tree);
  await user.click(screen.getByText("load"));
  return { user, written };
}

/** Run the flow with its saved input, so there is a result to promote. */
async function runOnce(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByLabelText("Run flow"));
  await user.click(await screen.findByText("happy path"));
  await screen.findByRole("button", { name: "Save as test case" });
}

/** The YAML the store was handed, once the debounced save has landed. */
async function savedYaml(written: { content: string }[]): Promise<string> {
  await waitFor(() => expect(written.length).toBeGreaterThan(0), { timeout: 3000 });
  return written[written.length - 1].content;
}

describe("SaveAsTestCase", () => {
  it("writes the run as a case in a suite the flow did not have", async () => {
    const { user, written } = await setup();
    await runOnce(user);

    await user.click(screen.getByRole("button", { name: "Save as test case" }));
    await user.click(screen.getByRole("button", { name: "Create the suite" }));

    const yaml = await savedYaml(written);
    expect(written[0].flow).toBe("orders");
    expect(yaml).toContain("flow: orders");
    expect(yaml).toContain("- name: it does what it did");
    // The input it ran with, as values rather than the JSON text a saved input holds.
    expect(yaml).toContain("orderId: 42");
  });

  /**
   * The trap this bridge exists to avoid. A run's result is the whole
   * `{event_id, variables, body}` envelope; a case asserting that as its body compares a
   * body against an envelope, and no flow ever produces one.
   */
  it("asserts the message's body, not the message", async () => {
    const { user, written } = await setup();
    await runOnce(user);

    await user.click(screen.getByRole("button", { name: "Save as test case" }));
    await user.click(screen.getByRole("button", { name: "Create the suite" }));

    const yaml = await savedYaml(written);
    expect(yaml).toContain("greeting: hi");
    expect(yaml).not.toContain("event_id");
  });

  // `expect.vars` is a subset check, so seeding it from every variable the run built up
  // asserts more than the user meant. It is offered, not assumed.
  it("leaves the variables out unless they are asked for", async () => {
    const { user, written } = await setup();
    await runOnce(user);

    await user.click(screen.getByRole("button", { name: "Save as test case" }));
    expect(await savedPreview()).not.toContain("tier: gold");

    await user.click(screen.getByLabelText("Also assert the variables it set"));
    await user.click(screen.getByRole("button", { name: "Create the suite" }));

    expect(await savedYaml(written)).toContain("tier: gold");
  });

  it("warns that the body is checked exactly", async () => {
    const { user } = await setup();
    await runOnce(user);

    await user.click(screen.getByRole("button", { name: "Save as test case" }));

    expect(screen.getByText(/checked exactly, field for field/)).toBeInTheDocument();
  });

  it("keeps the cases a suite already has", async () => {
    const { user, written } = await setup({
      suites: [{ flow: "orders", content: "flow: orders\ncases:\n  - name: it charges\n" }],
    });
    await runOnce(user);

    await user.click(screen.getByRole("button", { name: "Save as test case" }));
    await user.click(screen.getByRole("button", { name: "Add to the suite" }));

    const yaml = await savedYaml(written);
    expect(yaml).toContain("name: it charges");
    expect(yaml).toContain("name: it does what it did");
  });

  /**
   * A breakpoint run reports the message at that block, not the flow's result. Promoting
   * it would assert that the flow returns what it was carrying halfway through.
   */
  it("refuses a run that stopped at a block, and says why", async () => {
    const { user } = await setup();
    await user.click(screen.getByText("run to block"));

    const button = await screen.findByRole("button", { name: "Save as test case" });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("title", expect.stringContaining("stopped at a block"));
  });

  it("refuses to promote into a suite it could not fully read", async () => {
    const { user } = await setup({
      suites: [{ flow: "orders", content: "flow: orders\ncases:\n  - name: x\n    spys: {}\n" }],
    });
    await runOnce(user);

    const button = screen.getByRole("button", { name: "Save as test case" });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("title", expect.stringContaining("cannot read"));
  });

  /**
   * A failed run is worth promoting — the mocks that provoked the failure are the
   * valuable half — but what the console holds is the runner's stderr, timestamp and all.
   * Seeding `expect.error` from that would author a case that can never pass, so the
   * phrase is the user's to give and the save waits for it.
   */
  it("asks what the failure should contain instead of guessing", async () => {
    const { user, written } = await setup({ failed: true });
    await user.click(screen.getByLabelText("Run flow"));
    await user.click(await screen.findByText("happy path"));

    await user.click(await screen.findByRole("button", { name: "Save as test case" }));
    expect(screen.getByRole("button", { name: "Create the suite" })).toBeDisabled();

    await user.type(screen.getByPlaceholderText("card declined"), "card declined");
    await user.click(screen.getByRole("button", { name: "Create the suite" }));

    const yaml = await savedYaml(written);
    expect(yaml).toContain("error: card declined");
    expect(yaml).not.toContain("level=ERROR");
  });

  it("says a draft has nowhere to keep its tests", async () => {
    const { user } = await setup({ canEdit: false });
    await runOnce(user);

    const button = screen.getByRole("button", { name: "Save as test case" });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("title", "Save this integration to give it tests.");
  });

  it("offers nothing when the host keeps no suites", async () => {
    const { user } = await setup({ mounted: false });
    await user.click(screen.getByLabelText("Run flow"));
    await user.click(await screen.findByText("happy path"));

    await waitFor(() => expect(screen.getByText("orders")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Save as test case" })).toBeNull();
  });
});

/** The YAML preview the form shows before anything is written. */
async function savedPreview(): Promise<string> {
  const pre = await screen.findByText(/- name:/);
  return pre.textContent ?? "";
}
