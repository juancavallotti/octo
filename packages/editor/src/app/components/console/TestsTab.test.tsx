import { type ReactNode } from "react";
import { describe, it, expect } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { EditorStateProvider } from "../../state/editorState";
import { ConsoleProvider, useConsole } from "../../run/console";
import { RunProvider } from "../../run/RunContext";
import { SuiteRunProvider, useSuiteRun } from "../../run/SuiteRunContext";
import { emptyTotals, type TestRunOutcome } from "../../run/testTransport";
import type { RunTransport } from "../../run/transport";
import TestsTab from "./TestsTab";
import ConsoleTabs from "./ConsoleTabs";

const snapshot = {
  available: true,
  running: false,
  version: null,
  testAvailable: true,
  testVersion: null,
  testPath: null,
};

function transportWith(result: TestRunOutcome | Error): RunTransport {
  return {
    status: async () => snapshot,
    start: async () => snapshot,
    stop: async () => {},
    sync: async () => {},
    invoke: async () => ({ ok: true, dropped: false, timedOut: false, output: "", logs: [] }),
    evalCel: async () => ({ ok: true }),
    subscribeLogs: () => () => {},
    test: async () => {
      if (result instanceof Error) throw result;
      return result;
    },
  };
}

/** A run trigger plus the tab that shows it, and the strip that badges it. */
function Harness() {
  const run = useSuiteRun();
  const { tab } = useConsole();
  return (
    <>
      <button type="button" onClick={() => void run?.run("orders", "flow: orders\n")}>
        go
      </button>
      <p data-testid="tab">{tab}</p>
      <ConsoleTabs
        active={tab}
        onSelect={() => {}}
        counts={{
          tests: run?.outcome
            ? run.outcome.totals.failed + run.outcome.totals.errored
            : 0,
        }}
      />
      <TestsTab />
    </>
  );
}

function renderTab(transport: RunTransport) {
  const wrap = (children: ReactNode) => (
    <EditorStateProvider>
      <ConsoleProvider>
        <RunProvider transport={transport}>
          <SuiteRunProvider>{children}</SuiteRunProvider>
        </RunProvider>
      </ConsoleProvider>
    </EditorStateProvider>
  );
  return render(wrap(<Harness />));
}

const passing: TestRunOutcome = {
  ok: true,
  timedOut: false,
  totals: { ...emptyTotals(), cases: 1, passed: 1 },
  suites: [
    { name: "orders", flow: "orders", cases: [{ name: "it runs", status: "passed", elapsedMs: 3 }] },
  ],
  logs: [],
};

const failing: TestRunOutcome = {
  ok: true,
  timedOut: false,
  totals: { ...emptyTotals(), cases: 3, passed: 1, failed: 1, errored: 1 },
  suites: [
    {
      name: "orders",
      flow: "orders",
      cases: [
        { name: "it runs", status: "passed", elapsedMs: 3 },
        {
          name: "it charges",
          status: "failed",
          elapsedMs: 4,
          failures: [{ summary: "expect.body", detail: 'want: 1\ngot:  2' }],
        },
        { name: "it settles", status: "errored", elapsedMs: 1, error: "no such input" },
      ],
    },
  ],
  logs: [],
};

async function clickGo() {
  await act(async () => {
    screen.getByRole("button", { name: "go" }).click();
  });
}

describe("TestsTab", () => {
  it("says where results come from before the first run", () => {
    renderTab(transportWith(passing));

    expect(screen.getByText(/No test runs yet/)).toBeInTheDocument();
  });

  // A run of any kind reports in the console, so a suite run surfaces itself there the
  // way a flow run surfaces its output.
  it("opens the console on the Tests tab when a run starts", async () => {
    renderTab(transportWith(passing));
    expect(screen.getByTestId("tab")).toHaveTextContent("logs");

    await clickGo();

    expect(screen.getByTestId("tab")).toHaveTextContent("tests");
    expect(screen.getByText("it runs")).toBeInTheDocument();
  });

  // The badge counts what went wrong. A green run should be quiet — a badge reading "1"
  // beside a tab of passing tests reads as a problem.
  it("badges failures and errors, and nothing on a green run", async () => {
    const { unmount } = renderTab(transportWith(passing));
    await clickGo();
    const tests = screen.getByRole("button", { name: /Tests/ });
    expect(tests).toHaveTextContent(/^Tests$/);
    unmount();

    renderTab(transportWith(failing));
    await clickGo();
    expect(screen.getByRole("button", { name: /Tests/ })).toHaveTextContent("2");
  });

  it("shows each case, with a failure's detail preformatted", async () => {
    renderTab(transportWith(failing));

    await clickGo();

    expect(screen.getByText("it charges")).toBeInTheDocument();
    expect(screen.getByText("expect.body")).toBeInTheDocument();
    // A body diff is several lines; reflowing it destroys the alignment that makes it
    // readable, so it is rendered in a <pre>.
    expect(screen.getByText(/want: 1/).tagName).toBe("PRE");
    // An errored case is the suite being wrong, not the flow — said in those words.
    expect(screen.getByText("the case could not be run")).toBeInTheDocument();
  });

  // A rejection is not a report: the run never happened, and saying "0 passed" would be
  // a lie about a run that was never made.
  it("reports a run that could not be made", async () => {
    renderTab(transportWith(new Error("Test runner not available.")));

    await clickGo();

    expect(screen.getByRole("alert")).toHaveTextContent("Test runner not available.");
  });

  // The console has a fixed, user-dragged height and a report is as long as the run was,
  // so the tab owns a scroll region like every other one. Without it a long report grows
  // past the panel and the bottom of it cannot be reached at all.
  it("scrolls its own body", async () => {
    renderTab(transportWith(failing));
    await clickGo();

    const scroller = screen.getByText("it charges").closest(".overflow-auto");
    expect(scroller).toHaveClass("flex-1");
  });

  it("distinguishes a timeout from an unreadable report", async () => {
    renderTab(transportWith({ ...passing, ok: false, timedOut: true, suites: [] }));
    await clickGo();
    expect(screen.getByRole("alert")).toHaveTextContent(/took too long/);
  });
});
