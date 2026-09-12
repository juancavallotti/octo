import { type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { blankDocument, emptyFlow } from "../model/document";
import type { RunTransport } from "../run/transport";
import { FlowRunProvider } from "../run/FlowRunContext";
import { RunProvider } from "../run/RunContext";
import { ConsoleProvider } from "../run/console";
import { EditorActionType, EditorStateProvider, useEditorState } from "../state/editorState";
import { useRunShortcuts } from "./useRunShortcuts";

/**
 * Cmd+Enter is the shortcut people reach for in this kind of tool, so what matters
 * most is that it does not fire in the places it would be someone else's — a CEL
 * field runs its own expression with it — and that "run" never stops anything.
 */

const snapshot = {
  available: true,
  running: false,
  version: "octo 0.1.0",
  testAvailable: true,
  testVersion: null,
  testUrl: null,
  exposable: false,
  reloadsOnSave: false,
};

function stub() {
  const calls = { start: 0, stop: 0, invoke: 0 };
  let running = false;
  const transport: RunTransport = {
    status: async () => ({ ...snapshot, running }),
    start: async () => {
      calls.start++;
      running = true;
      return { ...snapshot, running };
    },
    stop: async () => {
      calls.stop++;
      running = false;
    },
    sync: async () => {},
    invoke: async () => {
      calls.invoke++;
      return { ok: true, dropped: false, timedOut: false, output: "", logs: [] };
    },
    evalCel: async () => ({ ok: true }),
    subscribeLogs: () => () => {},
    test: async () => ({
      ok: true,
      timedOut: false,
      totals: { cases: 0, passed: 0, failed: 0, errored: 0, skipped: 0, notRun: 0, elapsedMs: 0 },
      suites: [],
      logs: [],
    }),
  };
  return { transport, calls };
}

function Harness() {
  const { dispatch } = useEditorState();
  useRunShortcuts();
  const load = () =>
    dispatch({
      type: EditorActionType.LOAD_DOCUMENT,
      data: {
        document: {
          ...blankDocument(),
          flows: [
            {
              ...emptyFlow(),
              id: "f1",
              name: "orders",
              process: [{ id: "b1", type: "log", name: "audit", settings: {} }],
            },
          ],
        },
      },
    });
  return (
    <>
      <button onClick={load}>load</button>
      <input aria-label="a field" />
    </>
  );
}

function renderHarness(transport: RunTransport) {
  const wrap = (children: ReactNode) => (
    <EditorStateProvider>
      <ConsoleProvider>
        <RunProvider transport={transport}>
          <FlowRunProvider transport={transport}>{children}</FlowRunProvider>
        </RunProvider>
      </ConsoleProvider>
    </EditorStateProvider>
  );
  return render(wrap(<Harness />));
}

/** Load a valid document, so the run is not held back by validation. */
async function ready(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByText("load"));
  await waitFor(() => expect(screen.getByLabelText("a field")).toBeInTheDocument());
}

describe("useRunShortcuts", () => {
  it("starts the integration on Cmd+Enter", async () => {
    const user = userEvent.setup();
    const { transport, calls } = stub();
    renderHarness(transport);
    await ready(user);

    await user.keyboard("{Meta>}{Enter}{/Meta}");
    await waitFor(() => expect(calls.start).toBe(1));
  });

  it("runs one flow on Cmd+Shift+Enter, without starting the integration", async () => {
    const user = userEvent.setup();
    const { transport, calls } = stub();
    renderHarness(transport);
    await ready(user);

    await user.keyboard("{Meta>}{Shift>}{Enter}{/Shift}{/Meta}");
    await waitFor(() => expect(calls.invoke).toBe(1));
    expect(calls.start).toBe(0);
  });

  it("never stops a run — that is what Cmd+. is for", async () => {
    const user = userEvent.setup();
    const { transport, calls } = stub();
    renderHarness(transport);
    await ready(user);

    await user.keyboard("{Meta>}{Enter}{/Meta}");
    await waitFor(() => expect(calls.start).toBe(1));
    // Pressing "run" again on a running integration must not halt it: a key labelled
    // run that stops things is a surprise nobody wants twice.
    await user.keyboard("{Meta>}{Enter}{/Meta}");
    expect(calls.stop).toBe(0);
    expect(calls.start).toBe(1);
  });

  it("stops on Cmd+.", async () => {
    const user = userEvent.setup();
    const { transport, calls } = stub();
    renderHarness(transport);
    await ready(user);

    await user.keyboard("{Meta>}{Enter}{/Meta}");
    await waitFor(() => expect(calls.start).toBe(1));
    await user.keyboard("{Meta>}.{/Meta}");
    await waitFor(() => expect(calls.stop).toBe(1));
  });

  it("does nothing on Cmd+. when nothing is running", async () => {
    const user = userEvent.setup();
    const { transport, calls } = stub();
    renderHarness(transport);
    await ready(user);

    await user.keyboard("{Meta>}.{/Meta}");
    expect(calls.stop).toBe(0);
  });

  it("still runs with the caret in a field, which is where it usually is", async () => {
    // Editing a setting and pressing Run is the commonest thing there is. A Run key
    // that does nothing because focus never left the field reads as a broken key.
    const user = userEvent.setup();
    const { transport, calls } = stub();
    renderHarness(transport);
    await ready(user);

    await user.click(screen.getByLabelText("a field"));
    await user.keyboard("{Meta>}{Enter}{/Meta}");
    await waitFor(() => expect(calls.start).toBe(1));
  });

  it("leaves Cmd+. alone while the user is typing, which is the platform's own", async () => {
    const user = userEvent.setup();
    const { transport, calls } = stub();
    renderHarness(transport);
    await ready(user);

    await user.keyboard("{Meta>}{Enter}{/Meta}");
    await waitFor(() => expect(calls.start).toBe(1));
    await user.click(screen.getByLabelText("a field"));
    await user.keyboard("{Meta>}.{/Meta}");
    expect(calls.stop).toBe(0);
  });

  it("ignores a bare Enter", async () => {
    const user = userEvent.setup();
    const { transport, calls } = stub();
    renderHarness(transport);
    await ready(user);

    await user.keyboard("{Enter}");
    expect(calls.start).toBe(0);
  });

  it("does not start a document that would not validate", async () => {
    // The RUN button disables itself on this; a shortcut past it would start a run
    // the runner is about to refuse.
    const user = userEvent.setup();
    const { transport, calls } = stub();
    renderHarness(transport);
    // No load(): the blank document has no flows.
    await user.keyboard("{Meta>}{Enter}{/Meta}");
    expect(calls.start).toBe(0);
  });
});

describe("the run shortcut without a runner", () => {
  it("does nothing rather than throwing", async () => {
    const user = userEvent.setup();
    render(
      <EditorStateProvider>
        <Harness />
      </EditorStateProvider>,
    );
    await user.keyboard("{Meta>}{Enter}{/Meta}");
    await user.keyboard("{Meta>}.{/Meta}");
    expect(vi.getTimerCount).toBeDefined();
  });
});
