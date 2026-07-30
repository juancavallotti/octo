import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  EditorStateProvider,
  useEditorState,
  EditorActionType,
} from "../state/editorState";
import { emptyFlow, newBlock, type EditorDocument } from "../model/document";
import { ConsoleProvider, useConsole } from "./console";
import { FlowRunProvider, useFlowRun } from "./FlowRunContext";
import { emptyTotals } from "./transport";
import type { FlowRunOutcome, FlowRunRequest, RunTransport } from "./transport";
import {
  EditorMetaProvider,
  useEditorMeta,
} from "../providers/EditorMetaProvider";

/** A transport whose invoke returns whatever the test wants, and records the request. */
function stubTransport(
  outcome: Partial<FlowRunOutcome> = {},
  onInvoke?: (req: Parameters<RunTransport["invoke"]>[0]) => void,
): RunTransport {
  const snap = {
    available: true,
    running: false,
    version: null,
    // These fixtures exercise RUN, not the Testing tab: no dolphin configured.
    testAvailable: false,
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
    // These fixtures exercise RUN, not the Testing tab: no dolphin configured.
    test: async () => ({
      ok: false,
      timedOut: false,
      totals: emptyTotals(),
      suites: [],
      logs: [],
      error: "no test runner",
    }),
    invoke: async (req) => {
      onInvoke?.(req);
      return {
        ok: true,
        dropped: false,
        timedOut: false,
        // What `octo invoke` really prints: the result message, not the body alone.
        output:
          '{"event_id":"e1","variables":{"tier":"gold"},"body":{"greeting":"hi"}}',
        logs: [],
        ...outcome,
      };
    },
  };
}

/** A document with one flow ("orders") holding one named block. */
function seedDoc(): EditorDocument {
  const flow = emptyFlow("orders");
  const block = newBlock("log");
  block.name = "audit";
  flow.process = [block];
  return { flows: [flow], connectors: [], processors: [], env: [] };
}

/**
 * Loads a document into the reducer, then exposes buttons that drive the flow-run API
 * and read back what the console did.
 */
function Harness({ doc }: { doc: EditorDocument }) {
  const { state, dispatch } = useEditorState();
  const flowRun = useFlowRun();
  const { tab } = useConsole();

  const flow = state.document.flows[0];
  const block = flow?.process[0];

  return (
    <div>
      <button
        onClick={() =>
          dispatch({
            type: EditorActionType.LOAD_INTEGRATION,
            data: {
              id: "orders.yaml",
              name: "orders",
              document: doc,
              folderId: null,
            },
          })
        }
      >
        load
      </button>
      <button onClick={() => flow && flowRun?.runFlow(flow.id)}>
        run flow
      </button>
      <button onClick={() => flow && block && flowRun?.runToBlock(block.id)}>
        run to block
      </button>
      <p data-testid="tab">{tab}</p>
      <ul data-testid="results">
        {flowRun?.results.map((r) => (
          <li key={r.id}>
            {r.status}
            {r.breakpointLabel ? ` @${r.breakpointLabel}` : ""}
            {r.output !== undefined ? ` out=${JSON.stringify(r.output)}` : ""}
            {r.error ? ` err=${r.error}` : ""}
          </li>
        ))}
      </ul>
    </div>
  );
}

async function setup(transport: RunTransport) {
  const user = userEvent.setup();
  render(
    <EditorStateProvider>
      <ConsoleProvider>
        <FlowRunProvider transport={transport}>
          <Harness doc={seedDoc()} />
        </FlowRunProvider>
      </ConsoleProvider>
    </EditorStateProvider>,
  );
  await user.click(screen.getByText("load"));
  return user;
}

const results = () => screen.getByTestId("results").textContent ?? "";
const tab = () => screen.getByTestId("tab").textContent;

describe("FlowRunProvider", () => {
  // The whole result message is recorded, variables and all: a finished run must not
  // report less than the same run stopped at its last block.
  it("runs a flow and records its output", async () => {
    const user = await setup(stubTransport());
    await user.click(screen.getByText("run flow"));

    await waitFor(() => expect(results()).toContain("ok"));
    expect(results()).toContain('"body":{"greeting":"hi"}');
    expect(results()).toContain('"variables":{"tier":"gold"}');
  });

  it("shows the Results tab after a run", async () => {
    const user = await setup(stubTransport());
    expect(tab()).toBe("logs");

    await user.click(screen.getByText("run flow"));
    await waitFor(() => expect(tab()).toBe("results"));
  });

  // A failure is not output, so it belongs in Problems — that is where the user goes to
  // find out why nothing ran.
  it("shows the Problems tab when the run fails", async () => {
    const user = await setup(
      stubTransport({ ok: false, error: "Runner not available." }),
    );
    await user.click(screen.getByText("run flow"));

    await waitFor(() => expect(tab()).toBe("problems"));
    expect(results()).toContain("err=Runner not available.");
  });

  // A filtered message is a legitimate outcome, not an error: say so, and stay on
  // Results rather than crying failure.
  it("reports a dropped message as its own outcome", async () => {
    const user = await setup(stubTransport({ dropped: true, output: "" }));
    await user.click(screen.getByText("run flow"));

    await waitFor(() => expect(results()).toContain("dropped"));
    expect(tab()).toBe("results");
  });

  it("reports a timeout", async () => {
    const user = await setup(stubTransport({ timedOut: true }));
    await user.click(screen.getByText("run flow"));

    await waitFor(() => expect(results()).toContain("timeout"));
  });

  describe("run to a block", () => {
    it("passes the derived breakpoint address and surfaces the captured message", async () => {
      const seen: Parameters<RunTransport["invoke"]>[0][] = [];
      const user = await setup(
        stubTransport(
          {
            output: "",
            breakpoint: {
              reached: true,
              block: "orders.audit",
              message: { body: { amount: 250 } },
            },
          },
          (req) => seen.push(req),
        ),
      );

      await user.click(screen.getByText("run to block"));

      await waitFor(() => expect(results()).toContain("ok"));
      // The address came from planBreakpoint, derived from the block's position.
      expect(seen[0].breakAt).toBe("orders.audit");
      expect(seen[0].flow).toBe("orders");
      // The result is the message at the block, not a flow result body.
      expect(results()).toContain('out={"body":{"amount":250}}');
      expect(results()).toContain("@audit");
    });

    // The flow took another branch. That is an answer, not a failure — and the whole
    // reason someone sets a breakpoint on a branch.
    it("reports an unreached breakpoint as not-reached, not an error", async () => {
      const user = await setup(
        stubTransport({
          output: "",
          breakpoint: { reached: false, block: "orders.audit" },
        }),
      );
      await user.click(screen.getByText("run to block"));

      await waitFor(() => expect(results()).toContain("not-reached"));
      expect(tab()).toBe("results"); // not Problems: nothing went wrong
    });

    // A block inside a composite belongs to a sub-flow, which is not separately
    // runnable. runToBlock must find the root flow to invoke by itself — the caller (a
    // node on the canvas) knows only its block.
    it("runs the root flow for a block nested in a composite", async () => {
      const seen: Parameters<RunTransport["invoke"]>[0][] = [];
      const transport = stubTransport(
        { output: "", breakpoint: { reached: true, block: "x", message: {} } },
        (req) => seen.push(req),
      );

      // orders › if › then › [audit]
      const inner = newBlock("log");
      inner.name = "audit";
      const branch = emptyFlow("");
      branch.process = [inner];
      const iff = newBlock("if");
      iff.settings.condition = "body.ok";
      iff.slots!.then = [branch];
      const flow = emptyFlow("orders");
      flow.process = [iff];
      const doc: EditorDocument = {
        flows: [flow],
        connectors: [],
        processors: [],
        env: [],
      };

      function Nested() {
        const { state, dispatch } = useEditorState();
        const flowRun = useFlowRun();
        const target =
          state.document.flows[0]?.process[0]?.slots?.then?.[0]?.process[0];
        return (
          <div>
            <button
              onClick={() =>
                dispatch({
                  type: EditorActionType.LOAD_INTEGRATION,
                  data: {
                    id: "o.yaml",
                    name: "orders",
                    document: doc,
                    folderId: null,
                  },
                })
              }
            >
              load
            </button>
            {target && (
              <button onClick={() => flowRun?.runToBlock(target.id)}>
                run nested
              </button>
            )}
          </div>
        );
      }

      const user = userEvent.setup();
      render(
        <EditorStateProvider>
          <ConsoleProvider>
            <FlowRunProvider transport={transport}>
              <Nested />
            </FlowRunProvider>
          </ConsoleProvider>
        </EditorStateProvider>,
      );
      await user.click(screen.getByText("load"));
      await user.click(screen.getByText("run nested"));

      await waitFor(() => expect(seen).toHaveLength(1));
      expect(seen[0].flow).toBe("orders"); // the root flow, not the anonymous sub-flow
      expect(seen[0].breakAt).toBe("orders.if[then].audit");
    });

    it("reports a flow failure carried by the breakpoint envelope", async () => {
      const user = await setup(
        stubTransport({
          output: "",
          breakpoint: {
            reached: false,
            block: "orders.audit",
            error: 'block "audit": upstream refused',
          },
        }),
      );
      await user.click(screen.getByText("run to block"));

      await waitFor(() => expect(results()).toContain("error"));
      expect(results()).toContain("upstream refused");
      expect(tab()).toBe("problems");
    });
  });

  it("surfaces a transport that throws rather than losing the run", async () => {
    const transport = stubTransport();
    transport.invoke = vi.fn().mockRejectedValue(new Error("network down"));
    const user = await setup(transport);

    await user.click(screen.getByText("run flow"));

    await waitFor(() => expect(results()).toContain("err=network down"));
    expect(tab()).toBe("problems");
  });
});

/**
 * The same thing again, with the editor-meta capability mounted so mocks and spies exist.
 * The store is null, so they live for the session — which is all these tests need, and is
 * exactly what an unsaved draft gets in the real editor.
 */
function DebugHarness({ doc }: { doc: EditorDocument }) {
  const { state, dispatch } = useEditorState();
  const flowRun = useFlowRun();
  const meta = useEditorMeta();

  const flow = state.document.flows[0];

  return (
    <div>
      <button
        onClick={() =>
          dispatch({
            type: EditorActionType.LOAD_INTEGRATION,
            data: {
              id: "orders.yaml",
              name: "orders",
              document: doc,
              folderId: null,
            },
          })
        }
      >
        load
      </button>
      <button
        onClick={() => flow && meta?.setSpy(flow.id, "orders.audit", true)}
      >
        spy on audit
      </button>
      <button
        onClick={() =>
          flow &&
          meta?.setMock(flow.id, {
            address: "orders.audit",
            enabled: true,
            cases: [],
            default: { body: '{"stubbed": true}' },
          })
        }
      >
        mock audit
      </button>
      <button
        onClick={() =>
          flow && meta?.setSpy(flow.id, "orders.audit[then].inner", true)
        }
      >
        spy inside audit
      </button>
      <button onClick={() => flow && flowRun?.runFlow(flow.id)}>
        run flow
      </button>
      <button onClick={() => flowRun?.clearSpies()}>clear spies</button>
      <p data-testid="records">
        {(flowRun?.spyRecords("orders.audit") ?? [])
          .map(
            (r) =>
              `#${r.seq}:${JSON.stringify(r.output ?? r.error ?? "dropped")}`,
          )
          .join(" ")}
      </p>
      <ul data-testid="results">
        {flowRun?.results.map((r) => (
          <li key={r.id}>
            {r.status}
            {r.error ? ` err=${r.error}` : ""}
          </li>
        ))}
      </ul>
    </div>
  );
}

async function setupDebug(transport: RunTransport) {
  const user = userEvent.setup();
  render(
    <EditorStateProvider>
      <EditorMetaProvider store={null}>
        <ConsoleProvider>
          <FlowRunProvider transport={transport}>
            <DebugHarness doc={seedDoc()} />
          </FlowRunProvider>
        </ConsoleProvider>
      </EditorMetaProvider>
    </EditorStateProvider>,
  );
  await user.click(screen.getByText("load"));
  return user;
}

const records = () => screen.getByTestId("records").textContent ?? "";

describe("FlowRunProvider: mocks and spies", () => {
  /** A transport that returns one spy record per run, with an increasing seq. */
  function spyingTransport(
    onInvoke?: (req: FlowRunRequest) => void,
  ): RunTransport {
    let seq = 0;
    const t = stubTransport({}, onInvoke);
    const inner = t.invoke;
    t.invoke = async (req) => {
      const outcome = await inner(req);
      if (!req.spies?.length) return outcome;
      seq++;
      return {
        ...outcome,
        spies: req.spies.map((address) => ({
          address,
          records: [
            {
              seq,
              at: "2026-07-11T00:00:00Z",
              input: {},
              output: { run: seq },
            },
          ],
        })),
      };
    };
    return t;
  }

  it("sends the spied addresses and the enabled mocks with the run", async () => {
    let seen: FlowRunRequest | undefined;
    const user = await setupDebug(spyingTransport((req) => (seen = req)));

    await user.click(screen.getByText("spy on audit"));
    await user.click(screen.getByText("mock audit"));
    await user.click(screen.getByText("run flow"));

    await waitFor(() => expect(seen?.spies).toEqual(["orders.audit"]));
    // The meta file holds JSON text; the runner gets a value.
    expect(seen?.mocks).toEqual({
      "orders.audit": { default: { body: { stubbed: true } } },
    });
  });

  it("sends nothing when nothing is mocked or spied", async () => {
    let seen: FlowRunRequest | undefined;
    const user = await setupDebug(spyingTransport((req) => (seen = req)));

    await user.click(screen.getByText("run flow"));

    await waitFor(() => expect(seen).toBeDefined());
    expect(seen?.spies).toBeUndefined();
    expect(seen?.mocks).toBeUndefined();
  });

  /**
   * The point of the badge. A spy is left on while you iterate, and the interesting thing
   * is how the message differs between one run and the next — so resetting per run would
   * throw away the comparison the spy was turned on to make.
   */
  it("accumulates a spy's records across successive runs", async () => {
    const user = await setupDebug(spyingTransport());
    await user.click(screen.getByText("spy on audit"));

    await user.click(screen.getByText("run flow"));
    await waitFor(() => expect(records()).toContain('#1:{"run":1}'));

    await user.click(screen.getByText("run flow"));
    await waitFor(() => expect(records()).toContain('#2:{"run":2}'));
    // The first run's record is still there.
    expect(records()).toContain('#1:{"run":1}');
  });

  it("clears what the spies collected", async () => {
    const user = await setupDebug(spyingTransport());
    await user.click(screen.getByText("spy on audit"));
    await user.click(screen.getByText("run flow"));
    await waitFor(() => expect(records()).toContain("#1"));

    await user.click(screen.getByText("clear spies"));
    expect(records()).toBe("");
  });

  /**
   * A mock deletes the subtree it replaces, so a spy inside one can never fire. The
   * runtime rejects the run with `no block "x" in that chain`, which reads like a typo —
   * so we refuse before spending a run, and say what actually happened.
   */
  it("refuses a run whose spy sits inside a mocked block, without invoking", async () => {
    const invoked = vi.fn();
    const user = await setupDebug(spyingTransport(invoked));

    await user.click(screen.getByText("mock audit"));
    await user.click(screen.getByText("spy inside audit"));
    await user.click(screen.getByText("run flow"));

    await waitFor(() => expect(results()).toContain("inside the mocked block"));
    expect(invoked).not.toHaveBeenCalled();
  });
});
