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
import type { RunTransport } from "./transport";
import { RunProvider, useRun } from "./RunContext";

/**
 * What the provider does with the host's `reloadsOnSave` capability, which is the whole
 * of the difference between the two hosts on the client side.
 *
 * A pushing host (a local child process) runs whatever YAML it was last handed, so the
 * provider debounce-pushes every edit. A pulling host (a dev-run pod) reads the STORED
 * definition, so pushing a buffer is not merely redundant — nothing reads it, and the
 * panel would then imply an unsaved edit had taken effect.
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

/** A transport that records the configs pushed to it. */
function stubTransport(reloadsOnSave: boolean) {
  const syncs: string[] = [];
  const transport: RunTransport = {
    status: async () => snapshot(reloadsOnSave),
    start: async () => ({ ...snapshot(reloadsOnSave), running: true }),
    stop: async () => {},
    sync: async ({ yaml }) => {
      syncs.push(yaml);
    },
    invoke: async () => ({ ok: true, dropped: false, timedOut: false, output: "", logs: [] }),
    evalCel: async () => ({ ok: true }),
    subscribeLogs: () => () => {},
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
  return { transport, syncs };
}

/** Buttons for the three things a test needs to do: load, run, then edit. */
function Harness() {
  const run = useRun()!;
  const { dispatch } = useEditorState();
  // A flow with a step, because the provider only pushes a document that validates —
  // an empty flow would be held back for a reason that has nothing to do with this.
  const load = (flow: string) =>
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
  return (
    <>
      <button onClick={() => load("orders")}>load</button>
      <button onClick={() => void run.start()}>start</button>
      <button onClick={() => load("invoices")}>edit</button>
      <span data-testid="running">{String(run.running)}</span>
    </>
  );
}

function renderRun(reloadsOnSave: boolean) {
  const { transport, syncs } = stubTransport(reloadsOnSave);
  const tree = (children: ReactNode) => (
    <EditorStateProvider>
      <RunProvider transport={transport}>{children}</RunProvider>
    </EditorStateProvider>
  );
  render(tree(<Harness />));
  return { syncs };
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
