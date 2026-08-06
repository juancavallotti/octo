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
