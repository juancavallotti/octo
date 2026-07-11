import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorStateProvider, useEditorState, EditorActionType } from "../state/editorState";
import { emptyFlow, newBlock, type EditorDocument } from "../model/document";
import { ConsoleProvider, useConsole } from "./console";
import { FlowRunProvider, useFlowRun } from "./FlowRunContext";
import type { FlowRunOutcome, RunTransport } from "./transport";

/** A transport whose invoke returns whatever the test wants, and records the request. */
function stubTransport(
  outcome: Partial<FlowRunOutcome> = {},
  onInvoke?: (req: Parameters<RunTransport["invoke"]>[0]) => void,
): RunTransport {
  const snap = { available: true, running: false, version: null, testPath: null };
  return {
    status: async () => snap,
    start: async () => snap,
    stop: async () => {},
    sync: async () => {},
    evalCel: async () => ({ ok: true }),
    subscribeLogs: () => () => {},
    invoke: async (req) => {
      onInvoke?.(req);
      return {
        ok: true,
        dropped: false,
        timedOut: false,
        output: '{"greeting":"hi"}',
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
            data: { id: "orders.yaml", name: "orders", document: doc, folderId: null },
          })
        }
      >
        load
      </button>
      <button onClick={() => flow && flowRun?.runFlow(flow.id)}>run flow</button>
      <button onClick={() => flow && block && flowRun?.runToBlock(flow.id, block.id)}>
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
  it("runs a flow and records its output", async () => {
    const user = await setup(stubTransport());
    await user.click(screen.getByText("run flow"));

    await waitFor(() => expect(results()).toContain("ok"));
    expect(results()).toContain('out={"greeting":"hi"}');
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

    it("reports a flow failure carried by the breakpoint envelope", async () => {
      const user = await setup(
        stubTransport({
          output: "",
          breakpoint: {
            reached: false,
            block: "orders.audit",
            error: "block \"audit\": upstream refused",
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
