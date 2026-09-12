import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";
import { useEffect } from "react";
import {
  EditorActionType,
  EditorStateProvider,
  useEditorState,
} from "../state/editorState";
import { emptyFlow, newBlock, type EditorDocument } from "../model/document";
import { EditorMetaProvider } from "../providers/EditorMetaProvider";
import { EditorPrefsProvider } from "../prefs/prefs";
import AutoLearn from "./AutoLearn";
import { emptyTotals, type RunTransport } from "./transport";

/** A transport that records every invoke and answers with shapes. */
function stubTransport(
  calls: Parameters<RunTransport["invoke"]>[0][],
  celResult?: unknown,
): RunTransport {
  const snap = {
    available: true,
    running: false,
    version: null,
    testAvailable: false,
    testVersion: null,
    testUrl: null,
    exposable: false,
    reloadsOnSave: false,
  };
  return {
    status: async () => snap,
    start: async () => snap,
    stop: async () => {},
    sync: async () => {},
    evalCel: async () =>
      celResult === undefined
        ? { ok: false, error: "did not compile" }
        : { ok: true, result: celResult },
    subscribeLogs: () => () => {},
    test: async () => ({
      ok: false,
      timedOut: false,
      totals: emptyTotals(),
      suites: [],
      logs: [],
      error: "no test runner",
    }),
    invoke: async (req) => {
      calls.push(req);
      return { ok: true, dropped: false, timedOut: false, output: "{}", logs: [] };
    },
  };
}

/** One flow whose only block is the given type. */
function docWith(type: string): EditorDocument {
  const flow = emptyFlow("orders");
  flow.process = [newBlock(type)];
  return { flows: [flow], connectors: [], processors: [], env: [] };
}

/** Loads the document and makes its flow the active one, as opening a file does. */
function Load({ doc }: { doc: EditorDocument }) {
  const { dispatch } = useEditorState();
  useEffect(() => {
    dispatch({
      type: EditorActionType.LOAD_INTEGRATION,
      data: { id: "orders.yaml", name: "orders", document: doc, folderId: null },
    });
    dispatch({
      type: EditorActionType.SET_ACTIVE_FLOW,
      data: { flowId: doc.flows[0].id },
    });
  }, [dispatch, doc]);
  return null;
}

function tree(doc: EditorDocument, autoLearn: boolean, transport: RunTransport) {
  return (
    <EditorStateProvider>
      <EditorPrefsProvider prefs={{ autoLearn }}>
        <EditorMetaProvider store={null}>
          <Load doc={doc} />
          <AutoLearn transport={transport} />
        </EditorMetaProvider>
      </EditorPrefsProvider>
    </EditorStateProvider>
  );
}

function mount(doc: EditorDocument, autoLearn: boolean, transport: RunTransport) {
  const element = tree(doc, autoLearn, transport);
  const view = render(element);
  return { ...view, again: () => view.rerender(element) };
}

/** Past the settle delay, and past the promise the run resolves with. */
async function settle() {
  await act(async () => {
    vi.advanceTimersByTime(5000);
  });
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("AutoLearn", () => {
  it("runs a pure flow on its own and asks for shapes", async () => {
    const calls: Parameters<RunTransport["invoke"]>[0][] = [];
    mount(docWith("set-payload"), true, stubTransport(calls));
    await settle();
    expect(calls).toHaveLength(1);
    expect(calls[0].flow).toBe("orders");
    expect(calls[0].learnShapes).toBe(true);
  });

  it("stays out of the way when the preference is off", async () => {
    const calls: Parameters<RunTransport["invoke"]>[0][] = [];
    mount(docWith("set-payload"), false, stubTransport(calls));
    await settle();
    expect(calls).toHaveLength(0);
  });

  it("never runs a flow that would call out", async () => {
    const calls: Parameters<RunTransport["invoke"]>[0][] = [];
    mount(docWith("rest"), true, stubTransport(calls));
    await settle();
    expect(calls).toHaveLength(0);
  });

  it("feeds a source-driven flow the body its source would synthesize", async () => {
    // A cron flow's whole input is its payload expression. Running one with an empty
    // message teaches nothing downstream, because every expression reads a body that
    // was never there — which is the case that sent us looking at this at all.
    const calls: Parameters<RunTransport["invoke"]>[0][] = [];
    const doc = docWith("set-payload");
    doc.flows[0].source = {
      connector: "cron",
      type: "cron",
      settings: { schedule: "@every 1h", payload: '{"order": {"id": "a1"}}' },
    };
    mount(doc, true, stubTransport(calls, { order: { id: "a1" } }));
    await settle();
    expect(calls).toHaveLength(1);
    expect(calls[0].data).toBe('{"order":{"id":"a1"}}');
  });

  it("runs anyway when the payload will not evaluate", async () => {
    const calls: Parameters<RunTransport["invoke"]>[0][] = [];
    const doc = docWith("set-payload");
    doc.flows[0].source = {
      connector: "cron",
      type: "cron",
      settings: { schedule: "@every 1h", payload: "nonsense(" },
    };
    // An empty body is still worth a run, and there is nobody to report the failure to.
    mount(doc, true, stubTransport(calls));
    await settle();
    expect(calls).toHaveLength(1);
    expect(calls[0].data).toBeUndefined();
  });

  it("runs once per version of the flow, not once per render", async () => {
    const calls: Parameters<RunTransport["invoke"]>[0][] = [];
    const { again } = mount(docWith("set-payload"), true, stubTransport(calls));
    await settle();
    again();
    await settle();
    expect(calls).toHaveLength(1);
  });
});
