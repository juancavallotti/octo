import { describe, it, expect } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  EditorStateProvider,
  useEditorState,
  EditorActionType,
} from "../state/editorState";
import { emptyFlow, newBlock, type EditorDocument } from "../model/document";
import { EditorMetaProvider, type EditorMetaStore } from "../providers/EditorMetaProvider";
import {
  TestSuiteProvider,
  type TestSuiteStore,
} from "../providers/TestSuiteProvider";
import { ConsoleProvider } from "../run/console";
import { FlowRunProvider } from "../run/FlowRunContext";
import { RunProvider } from "../run/RunContext";
import { emptyTotals } from "../run/transport";
import type { FlowRunRequest, RunTransport } from "../run/transport";
import FlowRunMenu from "./FlowRunMenu";

/**
 * The first bridge: a flow's test cases offered in the ▶ menu as scenarios.
 *
 * What is being pinned here is not the list — it is that running a case from the canvas
 * and running it with `dolphin test` mean the same thing. The case's setup REPLACES the
 * canvas's mocks and spies, because that is dolphin's own rule; a merged run would give
 * the same scenario two verdicts depending on where it was started from.
 */

/**
 * A document with one flow ("orders") holding two named, addressable blocks. Two, so the
 * canvas can mock and spy a DIFFERENT block than the suite does — a per-address merge
 * would leave the canvas's showing, which is what the replacement test is looking for.
 */
function seedDoc(): EditorDocument {
  const flow = emptyFlow("orders");
  const audit = newBlock("log");
  audit.name = "audit";
  const notify = newBlock("log");
  notify.name = "notify";
  flow.process = [audit, notify];
  return { flows: [flow], connectors: [], processors: [], env: [] };
}

const SUITE = `flow: orders
inputs:
  vip:
    data: { orderId: 42 }
    vars: { tier: gold }
mocks:
  orders.audit:
    default:
      body: { charged: true }
cases:
  - name: a vip order
    input: vip
    spies:
      orders.audit:
        count: 1
  - name: an inline body
    input:
      data: { orderId: 7 }
  - name: not yet
    skip: waiting on the sandbox
`;

/** A canvas setup that must not survive a scenario run: another mock, another spy. */
const CANVAS_META = JSON.stringify({
  version: 1,
  resources: {
    "orders.yaml": {
      flows: {
        orders: {
          inputs: [],
          mocks: [
            {
              address: "orders.notify",
              enabled: true,
              cases: [],
              default: { body: '{"fromTheCanvas": true}' },
            },
          ],
          spies: ["orders.notify"],
        },
      },
    },
  },
});

function stubTransport(onInvoke: (req: FlowRunRequest) => void): RunTransport {
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
    invoke: async (req) => {
      onInvoke(req);
      return { ok: true, dropped: false, timedOut: false, output: "{}", logs: [] };
    },
  };
}

function suiteStore(files: { flow: string; content: string }[]): TestSuiteStore {
  return {
    list: async () => files,
    save: async () => {},
    remove: async () => {},
    canEdit: () => true,
  };
}

function metaStore(content: string): EditorMetaStore {
  return { load: async () => content, save: async () => {}, canEdit: () => true };
}

function Harness() {
  const { state, dispatch } = useEditorState();
  const flow = state.document.flows[0];
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
    </div>
  );
}

async function setup(
  opts: { suites?: { flow: string; content: string }[]; meta?: string } = {},
) {
  const user = userEvent.setup();
  const seen: FlowRunRequest[] = [];
  const transport = stubTransport((r) => seen.push(r));
  render(
    <EditorStateProvider>
      <EditorMetaProvider store={metaStore(opts.meta ?? "")}>
        <ConsoleProvider>
          <RunProvider transport={transport}>
            <FlowRunProvider transport={transport}>
              <TestSuiteProvider
                store={suiteStore(opts.suites ?? [{ flow: "orders", content: SUITE }])}
              >
                <Harness />
              </TestSuiteProvider>
            </FlowRunProvider>
          </RunProvider>
        </ConsoleProvider>
      </EditorMetaProvider>
    </EditorStateProvider>,
  );
  await user.click(screen.getByText("load"));
  await user.click(screen.getByLabelText("Run flow"));
  await screen.findByText("Scenarios");
  return { user, seen };
}

describe("FlowRunScenarios", () => {
  it("lists the suite's cases", async () => {
    await setup();

    expect(screen.getByText("a vip order")).toBeInTheDocument();
    expect(screen.getByText("an inline body")).toBeInTheDocument();
  });

  it("runs a case with its shared input resolved", async () => {
    const { user, seen } = await setup();

    await user.click(screen.getByText("a vip order"));

    await waitFor(() => expect(seen).toHaveLength(1));
    expect(seen[0].flow).toBe("orders");
    expect(seen[0].data).toBe('{"orderId":42}');
    // Sources do not run under invoke, so the variables a case seeds ride along too.
    expect(seen[0].vars).toBe('{"tier":"gold"}');
  });

  it("runs a case with an inline input", async () => {
    const { user, seen } = await setup();

    await user.click(screen.getByText("an inline body"));

    await waitFor(() => expect(seen).toHaveLength(1));
    expect(seen[0].data).toBe('{"orderId":7}');
    expect(seen[0].vars).toBeUndefined();
  });

  it("sends the file's mocks and the case's spies", async () => {
    const { user, seen } = await setup();

    await user.click(screen.getByText("a vip order"));

    await waitFor(() => expect(seen).toHaveLength(1));
    expect(seen[0].mocks).toEqual({
      "orders.audit": { default: { body: { charged: true } } },
    });
    expect(seen[0].spies).toEqual(["orders.audit"]);
  });

  /**
   * The whole point. A case is a complete statement of the world it runs in, so what the
   * canvas happens to have mocked and spied is not added to it — the run would otherwise
   * disagree with `dolphin test` on the same case, with nothing on screen to say why.
   */
  it("replaces the canvas's mocks and spies rather than merging them", async () => {
    const { user, seen } = await setup({ meta: CANVAS_META });

    await user.click(screen.getByText("an inline body"));

    await waitFor(() => expect(seen).toHaveLength(1));
    // Only the suite's mock. The canvas's mock on `orders.notify` is not carried along,
    // which a per-address merge — the tempting "improvement" — would have done.
    expect(seen[0].mocks).toEqual({
      "orders.audit": { default: { body: { charged: true } } },
    });
    // ...and the case watches nothing, so neither does the run.
    expect(seen[0].spies).toBeUndefined();
  });

  // A skipped case is skipped by the BATTERY. Running one by hand is exactly what you do
  // while fixing it, so it stays clickable.
  it("marks a skipped case but still runs it", async () => {
    const { user, seen } = await setup();

    expect(screen.getByLabelText("skipped")).toBeInTheDocument();
    await user.click(screen.getByText("not yet"));

    await waitFor(() => expect(seen).toHaveLength(1));
  });

  it("says the run does not assert anything", async () => {
    await setup();

    expect(
      screen.getByText(/Assertions are checked in the Testing tab/),
    ).toBeInTheDocument();
  });

  // dolphin passes a suite's `env:` to the child process; a flow run has no such channel,
  // so a case that stands on a fake credential behaves differently here.
  it("warns when the suite sets environment variables", async () => {
    await setup({
      suites: [
        {
          flow: "orders",
          content: "flow: orders\nenv:\n  API_KEY: fake\ncases:\n  - name: a vip order\n",
        },
      ],
    });

    expect(
      screen.getByText(/environment variables are not applied here/),
    ).toBeInTheDocument();
  });

  it("shows no scenarios for a flow with no suite", async () => {
    const user = userEvent.setup();
    const transport = stubTransport(() => {});
    render(
      <EditorStateProvider>
        <EditorMetaProvider store={metaStore("")}>
          <ConsoleProvider>
            <RunProvider transport={transport}>
              <FlowRunProvider transport={transport}>
                <TestSuiteProvider store={suiteStore([])}>
                  <Harness />
                </TestSuiteProvider>
              </FlowRunProvider>
            </RunProvider>
          </ConsoleProvider>
        </EditorMetaProvider>
      </EditorStateProvider>,
    );
    await user.click(screen.getByText("load"));
    await user.click(screen.getByLabelText("Run flow"));

    expect(screen.getByText("No input")).toBeInTheDocument();
    expect(screen.queryByText("Scenarios")).not.toBeInTheDocument();
  });
});
