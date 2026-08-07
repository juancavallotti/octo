import { type ReactNode } from "react";
import { describe, it, expect } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  EditorStateProvider,
  useEditorState,
  EditorActionType,
} from "../state/editorState";
import { blankDocument, emptyFlow } from "../model/document";
import type { RunTarget, RunTransport } from "./transport";
import { RunProvider, useRun } from "./RunContext";

/**
 * Two things the provider owes a host, both of which are the whole of what differs between
 * the two hosts on the client side.
 *
 * `reloadsOnSave` is the capability. A pushing host (a local child process) runs whatever
 * YAML it was last handed, so the provider debounce-pushes every edit. A pulling host (a
 * dev-run pod) reads the STORED definition, so pushing a buffer is not merely redundant —
 * nothing reads it, and the panel would then imply an unsaved edit had taken effect.
 *
 * The run target is the address. A host that runs the app elsewhere has nothing but the
 * open integration to name the run by, so every call has to carry it — not only the ones
 * that need its resources.
 */

/** How long the provider waits before pushing an edit, plus head-room. */
const PAST_DEBOUNCE_MS = 2500;

function snapshot(reloadsOnSave: boolean) {
  return {
    available: true,
    running: false,
    version: "octo 0.1.0",
    testAvailable: false,
    testVersion: null,
    testUrl: null,
    exposable: false,
    reloadsOnSave,
  };
}

/** A transport that records the configs pushed to it, and who each call addressed. */
function stubTransport(reloadsOnSave: boolean) {
  const syncs: string[] = [];
  /** Every call's target, keyed by the method that made it. */
  const targets: Record<string, RunTarget> = {};
  const transport: RunTransport = {
    status: async (target) => {
      targets.status = target;
      return snapshot(reloadsOnSave);
    },
    start: async ({ yaml: _yaml, ...target }) => {
      targets.start = target;
      return { ...snapshot(reloadsOnSave), running: true };
    },
    stop: async (target) => {
      targets.stop = target;
    },
    sync: async ({ yaml, ...target }) => {
      targets.sync = target;
      syncs.push(yaml);
    },
    invoke: async () => ({ ok: true, dropped: false, timedOut: false, output: "", logs: [] }),
    evalCel: async () => ({ ok: true }),
    subscribeLogs: (_onLine, target) => {
      targets.subscribeLogs = target;
      return () => {};
    },
    test: async () => ({
      ok: true,
      timedOut: false,
      totals: {
        cases: 0,
        passed: 0,
        failed: 0,
        errored: 0,
        skipped: 0,
        notRun: 0,
        elapsedMs: 0,
      },
      suites: [],
      logs: [],
    }),
  };
  return { transport, syncs, targets };
}

/** Buttons for the three things a test needs to do: load, run, then edit. */
function Harness({ integrationId }: { integrationId?: string }) {
  const run = useRun()!;
  const { dispatch } = useEditorState();
  // A flow with a step, because the provider only pushes a document that validates —
  // an empty flow would be held back for a reason that has nothing to do with this.
  const load = (flow: string) => {
    // Set alongside the document rather than once on mount, so a test observes the
    // provider going from "a draft" to "a saved integration" the way the editor does.
    if (integrationId) {
      dispatch({ type: EditorActionType.SET_INTEGRATION_ID, data: { id: integrationId } });
    }
    dispatch({
      type: EditorActionType.LOAD_DOCUMENT,
      data: {
        document: {
          ...blankDocument(),
          flows: [
            {
              ...emptyFlow(),
              name: flow,
              process: [{ id: "b1", type: "log", name: "audit", settings: {} }],
            },
          ],
        },
      },
    });
  };
  return (
    <>
      <button onClick={() => load("orders")}>load</button>
      <button onClick={() => void run.start()}>start</button>
      <button onClick={() => void run.stop()}>stop</button>
      <button onClick={() => load("invoices")}>edit</button>
      <span data-testid="running">{String(run.running)}</span>
      <span data-testid="url">{String(run.testUrl)}</span>
    </>
  );
}

function renderRun(reloadsOnSave: boolean, integrationId?: string) {
  const { transport, syncs, targets } = stubTransport(reloadsOnSave);
  const tree = (children: ReactNode) => (
    <EditorStateProvider>
      <RunProvider transport={transport}>{children}</RunProvider>
    </EditorStateProvider>
  );
  render(tree(<Harness integrationId={integrationId} />));
  return { syncs, targets };
}

/** Load a document, start the run, then change it. */
async function loadStartEdit(): Promise<void> {
  const user = userEvent.setup();
  await user.click(screen.getByText("load"));
  await user.click(screen.getByText("start"));
  await waitFor(() => expect(screen.getByTestId("running")).toHaveTextContent("true"));
  await user.click(screen.getByText("edit"));
}

describe("RunProvider and the host's reload model", () => {
  it("pushes a debounced edit to a host that runs what it is handed", async () => {
    const { syncs } = renderRun(false);
    await loadStartEdit();

    await waitFor(() => expect(syncs).toHaveLength(1), { timeout: PAST_DEBOUNCE_MS });
    expect(syncs[0]).toContain("invoices");
  }, 10000);

  it("pushes nothing to a host that reloads on save", async () => {
    const { syncs } = renderRun(true);
    await loadStartEdit();

    // Waited out rather than merely checked: the point is that the push never happens,
    // and a synchronous assertion would pass even against a provider that was about to
    // make one.
    await new Promise((resolve) => setTimeout(resolve, PAST_DEBOUNCE_MS));
    expect(syncs).toEqual([]);
  }, 10000);
});

describe("RunProvider and the run's address", () => {
  // Every method, not just the ones that need the integration's resources: on a host that
  // runs the app elsewhere, one that arrived without it could not find the run at all.
  it("tells the host which run every call addresses", async () => {
    const { targets } = renderRun(false, "int-42");
    const user = userEvent.setup();
    await user.click(screen.getByText("load"));
    await waitFor(() => expect(targets.status).toBeDefined());
    await user.click(screen.getByText("start"));
    await waitFor(() => expect(screen.getByTestId("running")).toHaveTextContent("true"));
    await user.click(screen.getByText("edit"));
    await waitFor(() => expect(targets.sync).toBeDefined(), { timeout: PAST_DEBOUNCE_MS });
    await user.click(screen.getByText("stop"));

    await waitFor(() =>
      expect(targets).toEqual({
        status: { integrationId: "int-42" },
        start: { integrationId: "int-42" },
        subscribeLogs: { integrationId: "int-42" },
        sync: { integrationId: "int-42" },
        stop: { integrationId: "int-42" },
      }),
    );
  }, 10000);

  // A draft has no id, and the target says so rather than being absent: a host that can
  // only run a saved integration has to be able to refuse, and it needs to be asked first.
  it("addresses an unsaved draft with no integration at all", async () => {
    const { targets } = renderRun(false);
    await waitFor(() => expect(targets.status).toEqual({ integrationId: undefined }));
  });
});

describe("RunProvider and switching the open integration", () => {
  // Switching which integration is open must not leave the previous run's stream open or
  // its `running` state stuck. openStream refuses a second stream while one is live, so a
  // stale stream would keep showing the old integration's logs; a stuck `running` would
  // make Stop target the newly-opened integration while the old run kept going.
  it("closes the old stream and resets running when the target changes", async () => {
    let unsubscribed = 0;
    const statusTargets: (string | undefined)[] = [];
    const transport: RunTransport = {
      status: async (target) => {
        statusTargets.push(target.integrationId);
        // int-a is already running (reattach on open); int-b is not.
        return { ...snapshot(true), running: target.integrationId === "int-a" };
      },
      start: async () => ({ ...snapshot(true), running: true }),
      stop: async () => {},
      sync: async () => {},
      invoke: async () => ({ ok: true, dropped: false, timedOut: false, output: "", logs: [] }),
      evalCel: async () => ({ ok: true }),
      subscribeLogs: () => () => {
        unsubscribed += 1;
      },
      test: async () => ({
        ok: true,
        timedOut: false,
        totals: { cases: 0, passed: 0, failed: 0, errored: 0, skipped: 0, notRun: 0, elapsedMs: 0 },
        suites: [],
        logs: [],
      }),
    };

    function Switcher() {
      const run = useRun()!;
      const { dispatch } = useEditorState();
      const open = (id: string) =>
        dispatch({ type: EditorActionType.SET_INTEGRATION_ID, data: { id } });
      return (
        <>
          <button onClick={() => open("int-a")}>open-a</button>
          <button onClick={() => open("int-b")}>open-b</button>
          <span data-testid="running">{String(run.running)}</span>
        </>
      );
    }

    render(
      <EditorStateProvider>
        <RunProvider transport={transport}>
          <Switcher />
        </RunProvider>
      </EditorStateProvider>,
    );

    const user = userEvent.setup();
    // Open int-a: status reports it running, so the provider reattaches and opens a stream.
    await user.click(screen.getByText("open-a"));
    await waitFor(() => expect(screen.getByTestId("running")).toHaveTextContent("true"));

    // Switch to int-b, which is not running.
    await user.click(screen.getByText("open-b"));

    // The old stream was torn down, and running reflects int-b rather than sticking true.
    await waitFor(() => expect(screen.getByTestId("running")).toHaveTextContent("false"));
    expect(unsubscribed).toBeGreaterThanOrEqual(1);
    expect(statusTargets).toContain("int-b");
  }, 10000);
});

describe("RunProvider and a networked run still coming up", () => {
  // A dev-run pod withholds its public URL until it is ready, so start returns none and the
  // URL is learned only from a later status read. Status is otherwise read once, on mount —
  // so without a poll the log panel's endpoint link would stay blank from the click of Run
  // until a full page reload. The run reports itself exposable meanwhile, which is the signal
  // that a null URL is coming rather than never.
  it("reveals the URL from a later status read, without a reload", async () => {
    const url = "https://orders.run.example/";
    let started = false;
    const transport: RunTransport = {
      // Not running until start; once started the run is exposable, and from the first poll
      // after it, its URL is available — the very URL start itself withheld.
      status: async () =>
        started
          ? { ...snapshot(true), running: true, exposable: true, testUrl: url }
          : snapshot(true),
      start: async () => {
        started = true;
        return { ...snapshot(true), running: true, exposable: true, testUrl: null };
      },
      stop: async () => {},
      sync: async () => {},
      invoke: async () => ({ ok: true, dropped: false, timedOut: false, output: "", logs: [] }),
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

    render(
      <EditorStateProvider>
        <RunProvider transport={transport}>
          <Harness integrationId="int-1" />
        </RunProvider>
      </EditorStateProvider>,
    );

    const user = userEvent.setup();
    await user.click(screen.getByText("load"));
    await user.click(screen.getByText("start"));
    await waitFor(() => expect(screen.getByTestId("running")).toHaveTextContent("true"));

    // Withheld at start: the pod is not ready, so there is no link to offer yet.
    expect(screen.getByTestId("url")).toHaveTextContent("null");

    // Learned on its own from a later status read — the fix for the link that used to appear
    // only after the window was reloaded.
    await waitFor(() => expect(screen.getByTestId("url")).toHaveTextContent(url), {
      timeout: 6000,
    });
  }, 12000);
});
