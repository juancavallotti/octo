import { type ReactNode } from "react";
import { describe, it, expect } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import { EditorStateProvider } from "../state/editorState";
import { RunProvider } from "./RunContext";
import { ConsoleProvider } from "./console";
import { emptyTotals, type TestRunOutcome } from "./testTransport";
import type { RunTransport } from "./transport";
import { SuiteRunProvider, useSuiteRun } from "./SuiteRunContext";

/**
 * The provider on its own. The Testing tab drives it through a button that disables
 * itself while a run is in flight — which means the tab's tests cannot reach the guard
 * against a SECOND call arriving before the state update that disables it. This can.
 */

const snapshot = {
  available: true,
  running: false,
  version: null,
  testAvailable: true,
  testVersion: null,
  testUrl: null,
  reloadsOnSave: false,
};

function outcome(): TestRunOutcome {
  return {
    ok: true,
    timedOut: false,
    totals: { ...emptyTotals(), cases: 1, passed: 1 },
    suites: [],
    logs: [],
  };
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

function harness(test: RunTransport["test"]) {
  const transport: RunTransport = {
    status: async () => snapshot,
    start: async () => snapshot,
    stop: async () => {},
    sync: async () => {},
    invoke: async () => ({ ok: true, dropped: false, timedOut: false, output: "", logs: [] }),
    evalCel: async () => ({ ok: true }),
    subscribeLogs: () => () => {},
    test,
  };
  const wrapper = ({ children }: { children: ReactNode }) => (
    <EditorStateProvider>
      <ConsoleProvider>
        <RunProvider transport={transport}>
          <SuiteRunProvider>{children}</SuiteRunProvider>
        </RunProvider>
      </ConsoleProvider>
    </EditorStateProvider>
  );
  // Non-null throughout: the provider is mounted, so the context is never absent here.
  return renderHook(() => useSuiteRun()!, { wrapper });
}

describe("useSuiteRun", () => {
  // Two runs would not corrupt each other — the server is stateless — but they would
  // produce two reports for a screen that shows one, and the later one might land first.
  it("ignores a second run started before the first has settled", async () => {
    let calls = 0;
    let release!: () => void;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    const { result } = harness(async () => {
      calls++;
      await pending;
      return outcome();
    });

    await act(async () => {
      void result.current.run([{ name: "orders", content: "flow: orders\n" }]);
      void result.current.run([{ name: "orders", content: "flow: orders\n" }]);
    });

    expect(calls).toBe(1);

    await act(async () => {
      release();
    });
    await waitFor(() => expect(result.current.running).toBe(false));
    expect(result.current.outcome).not.toBeNull();
  });

  // A rejection is not a report. Leaving a stale one on screen beside a new error would
  // read as though the failing run had produced it.
  it("clears the last report when a run cannot be made", async () => {
    let fail = false;
    const { result } = harness(async () => {
      if (fail) throw new Error("no runner");
      return outcome();
    });

    await act(async () => {
      await result.current.run([{ name: "orders", content: "flow: orders\n" }]);
    });
    expect(result.current.outcome).not.toBeNull();

    fail = true;
    await act(async () => {
      await result.current.run([{ name: "orders", content: "flow: orders\n" }]);
    });

    expect(result.current.outcome).toBeNull();
    expect(result.current.error).toBe("no runner");
  });

  it("clears a previous error when a later run succeeds", async () => {
    let fail = true;
    const { result } = harness(async () => {
      if (fail) throw new Error("no runner");
      return outcome();
    });

    await act(async () => {
      await result.current.run([{ name: "orders", content: "flow: orders\n" }]);
    });
    expect(result.current.error).toBe("no runner");

    fail = false;
    await act(async () => {
      await result.current.run([{ name: "orders", content: "flow: orders\n" }]);
    });

    expect(result.current.error).toBeNull();
    expect(result.current.outcome).not.toBeNull();
  });

  // Every suite in one request, not one request per suite: dolphin runs them against a
  // single staged config, and N round trips would each stage and tear down their own.
  it("sends every suite it was given in one request", async () => {
    const sent: string[][] = [];
    const { result } = harness(async (req) => {
      sent.push(req.suites.map((s) => s.name));
      return outcome();
    });

    await act(async () => {
      await result.current.run([
        { name: "orders", content: "flow: orders\n" },
        { name: "refunds", content: "flow: refunds\n" },
      ]);
    });

    expect(sent).toEqual([["orders", "refunds"]]);
    expect(result.current.flows).toEqual(["orders", "refunds"]);
  });

  // The suites a caller held back are part of the verdict: a tally of two passing suites
  // means something different when three others never ran.
  it("carries what the caller left out, so the console can say so", async () => {
    const { result } = harness(async () => outcome());

    await act(async () => {
      await result.current.run(
        [{ name: "orders", content: "flow: orders\n" }],
        [{ flow: "refunds", reason: "has no cases yet" }],
      );
    });

    expect(result.current.skipped).toEqual([
      { flow: "refunds", reason: "has no cases yet" },
    ]);
  });

  // The host rejects an empty request, and every caller disables its button in that state
  // — so reaching the transport at all could only produce an error about nothing.
  it("does not call the runner with no suites to run", async () => {
    let calls = 0;
    const { result } = harness(async () => {
      calls++;
      return outcome();
    });

    await act(async () => {
      await result.current.run([]);
    });

    expect(calls).toBe(0);
    expect(result.current.running).toBe(false);
    expect(result.current.error).toBeNull();
  });

  it("forgets everything on clear", async () => {
    const { result } = harness(async () => outcome());

    await act(async () => {
      await result.current.run([{ name: "orders", content: "flow: orders\n" }]);
    });
    act(() => result.current.clear());

    expect(result.current.outcome).toBeNull();
    expect(result.current.error).toBeNull();
    expect(result.current.flows).toEqual([]);
    expect(result.current.skipped).toEqual([]);
  });

  it("ignores a stale completion after clear while running", async () => {
    const gate = deferred<void>();
    const { result } = harness(async () => {
      await gate.promise;
      return outcome();
    });

    await act(async () => {
      void result.current.run([{ name: "orders", content: "flow: orders\n" }]);
    });
    expect(result.current.running).toBe(true);

    act(() => result.current.clear());
    expect(result.current.running).toBe(false);
    expect(result.current.outcome).toBeNull();
    expect(result.current.error).toBeNull();
    expect(result.current.flows).toEqual([]);

    await act(async () => {
      gate.resolve();
    });
    await waitFor(() => expect(result.current.running).toBe(false));

    expect(result.current.outcome).toBeNull();
    expect(result.current.error).toBeNull();
    expect(result.current.flows).toEqual([]);
  });
});
