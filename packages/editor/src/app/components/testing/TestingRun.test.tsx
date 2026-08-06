import { useEffect, type ReactNode } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  EditorStateProvider,
  useEditorState,
  EditorActionType,
} from "../../state/editorState";
import {
  TestSuiteProvider,
  type TestSuiteFile,
  type TestSuiteStore,
} from "../../providers/TestSuiteProvider";
import { RunProvider } from "../../run/RunContext";
import { ConsoleProvider } from "../../run/console";
import { SuiteRunProvider } from "../../run/SuiteRunContext";
import LogPanel from "../LogPanel";
import {
  emptyTotals,
  type TestRunOutcome,
  type TestRunRequest,
} from "../../run/testTransport";
import type { RunTransport } from "../../run/transport";
import { blankDocument, emptyFlow } from "../../model/document";
import TestingView from "./TestingView";

const snapshot = (testAvailable: boolean) => ({
  available: true,
  running: false,
  version: "octo 0.1.0",
  testAvailable,
  testVersion: testAvailable ? "dolphin 0.1.0" : null,
  testUrl: null,
  reloadsOnSave: false,
});

/** A transport whose test() returns what the test wants, and records the request. */
function stubTransport(
  result: TestRunOutcome | Error | Promise<TestRunOutcome>,
  { testAvailable = true }: { testAvailable?: boolean } = {},
) {
  const requests: TestRunRequest[] = [];
  const transport: RunTransport = {
    status: async () => snapshot(testAvailable),
    start: async () => snapshot(testAvailable),
    stop: async () => {},
    sync: async () => {},
    invoke: async () => ({ ok: true, dropped: false, timedOut: false, output: "", logs: [] }),
    evalCel: async () => ({ ok: true, result: true }),
    subscribeLogs: () => () => {},
    test: async (req) => {
      requests.push(req);
      if (result instanceof Error) throw result;
      return result;
    },
  };
  return { transport, requests };
}

function outcome(over: Partial<TestRunOutcome> = {}): TestRunOutcome {
  return {
    ok: true,
    timedOut: false,
    totals: { ...emptyTotals(), cases: 1, passed: 1, elapsedMs: 12 },
    suites: [
      {
        name: "orders",
        flow: "orders",
        cases: [{ name: "it runs", status: "passed", elapsedMs: 12 }],
      },
    ],
    logs: [],
    ...over,
  };
}

const suiteYaml = "flow: orders\ncases:\n  - name: it runs\n    expect: {}\n";

function fakeStore(files: TestSuiteFile[]) {
  const map = new Map(files.map((f) => [f.flow, f.content]));
  return {
    list: async () => [...map].map(([flow, content]) => ({ flow, content })),
    save: async () => {},
    remove: async () => {},
    canEdit: () => true,
  } as TestSuiteStore;
}

function renderTab(
  transport: RunTransport,
  store: TestSuiteStore,
  flows = ["orders"],
) {
  function Seed({ children }: { children: ReactNode }) {
    const { dispatch } = useEditorState();
    useEffect(() => {
      dispatch({
        type: EditorActionType.LOAD_DOCUMENT,
        data: {
          document: {
            ...blankDocument(),
            flows: flows.map((name) => ({ ...emptyFlow(), name })),
          },
        },
      });
      dispatch({ type: EditorActionType.SET_INTEGRATION_ID, data: { id: "one" } });
    }, [dispatch]);
    return <>{children}</>;
  }
  return render(
    <EditorStateProvider>
      <Seed>
        <ConsoleProvider>
          <RunProvider transport={transport}>
            <SuiteRunProvider>
              <TestSuiteProvider store={store}>
                {/* The tab starts the run; the console shows what came back, exactly as
                    the editor wires them. */}
                <TestingView />
                <LogPanel />
              </TestSuiteProvider>
            </SuiteRunProvider>
          </RunProvider>
        </ConsoleProvider>
      </Seed>
    </EditorStateProvider>,
  );
}

/** Open the suite for `flow` and return the Run button. */
async function openSuite(user: ReturnType<typeof userEvent.setup>, flow = "orders") {
  await user.click(await screen.findByRole("button", { name: flow }));
  return screen.getByRole("button", { name: /Run tests/ });
}

/**
 * The open suite's toolbar. A tally is scoped to it because the console reports one per
 * suite too, so "1 passed" on its own no longer says whose.
 */
const toolbar = () => screen.getByRole("button", { name: /Run tests/ }).parentElement!;

describe("running a suite", () => {
  it("sends the open suite and the current document", async () => {
    const user = userEvent.setup();
    const { transport, requests } = stubTransport(outcome());
    renderTab(transport, fakeStore([{ flow: "orders", content: suiteYaml }]));

    await user.click(await openSuite(user));

    await waitFor(() => expect(requests).toHaveLength(1));
    expect(requests[0].suites).toEqual([{ name: "orders", content: suiteYaml }]);
    expect(requests[0].integrationId).toBe("one");
    // The config is rendered from the document in front of the user, not from what was
    // last saved — testing yesterday's copy would be worse than useless.
    expect(requests[0].yaml).toContain("orders");
  });

  it("shows the tally and each case's verdict", async () => {
    const user = userEvent.setup();
    const { transport } = stubTransport(
      outcome({
        totals: { ...emptyTotals(), cases: 2, passed: 1, failed: 1, elapsedMs: 30 },
        suites: [
          {
            name: "orders",
            flow: "orders",
            cases: [
              { name: "it runs", status: "passed", elapsedMs: 12 },
              {
                name: "it charges",
                status: "failed",
                elapsedMs: 18,
                failures: [
                  { summary: "expect.body", detail: 'want: {"a":1}\ngot:  {"a":2}' },
                ],
                outcome: { result: { body: { a: 2 } } },
              },
            ],
          },
        ],
      }),
    );
    renderTab(transport, fakeStore([{ flow: "orders", content: suiteYaml }]));

    await user.click(await openSuite(user));

    await waitFor(() =>
      expect(within(toolbar()).getByText("1 failed")).toBeInTheDocument(),
    );
    expect(within(toolbar()).getByText("1 passed")).toBeInTheDocument();
    expect(screen.getByText("it charges")).toBeInTheDocument();
    // A failure opens itself: it is the reason the button was pressed. The detail is a
    // diff, so it must survive with its newlines.
    expect(screen.getByText("expect.body")).toBeInTheDocument();
    expect(screen.getByText(/want: \{"a":1\}/)).toBeInTheDocument();
    // ...beside what the flow actually produced.
    expect(screen.getByText("the flow returned")).toBeInTheDocument();
  });

  // dolphin exits non-zero for a failing case. If that were treated as a broken run the
  // user would be told "the run failed" instead of shown the test that failed.
  it("treats a failing test as a successful run", async () => {
    const user = userEvent.setup();
    const { transport } = stubTransport(
      outcome({
        totals: { ...emptyTotals(), cases: 1, failed: 1 },
        suites: [
          {
            name: "orders",
            flow: "orders",
            cases: [{ name: "it runs", status: "failed", elapsedMs: 5 }],
          },
        ],
      }),
    );
    renderTab(transport, fakeStore([{ flow: "orders", content: suiteYaml }]));

    await user.click(await openSuite(user));

    await waitFor(() =>
      expect(within(toolbar()).getByText("1 failed")).toBeInTheDocument(),
    );
    expect(screen.queryByRole("alert")).toBeNull();
  });

  // A rejection means the call could not be made at all — a different thing from a run
  // whose tests failed, and it must not be reported as one.
  it("reports a run that could not be made", async () => {
    const user = userEvent.setup();
    const { transport } = stubTransport(new Error("Test runner not available."));
    renderTab(transport, fakeStore([{ flow: "orders", content: suiteYaml }]));

    await user.click(await openSuite(user));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Test runner not available.",
    );
  });

  it("reports a run that produced no readable report", async () => {
    const user = userEvent.setup();
    const { transport } = stubTransport(
      outcome({ ok: false, error: "no report was written", suites: [] }),
    );
    renderTab(transport, fakeStore([{ flow: "orders", content: suiteYaml }]));

    await user.click(await openSuite(user));

    expect(await screen.findByRole("alert")).toHaveTextContent("no report was written");
  });

  it("says a timed-out run was stopped rather than showing a bare error", async () => {
    const user = userEvent.setup();
    const { transport } = stubTransport(outcome({ ok: false, timedOut: true, suites: [] }));
    renderTab(transport, fakeStore([{ flow: "orders", content: suiteYaml }]));

    await user.click(await openSuite(user));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The run took too long and was stopped.",
    );
  });
});

describe("when running is not possible", () => {
  // The button stays put and explains itself. Hiding it would leave someone who just
  // wrote a suite with no way to learn why they cannot run it.
  it("keeps Run visible and says why, with no dolphin binary", async () => {
    const user = userEvent.setup();
    const { transport, requests } = stubTransport(outcome(), { testAvailable: false });
    renderTab(transport, fakeStore([{ flow: "orders", content: suiteYaml }]));

    const button = await openSuite(user);
    await waitFor(() => expect(button).toBeDisabled());
    expect(button).toHaveAttribute("title", expect.stringContaining("DOLPHIN_BIN_PATH"));

    await user.click(button);
    expect(requests).toHaveLength(0);
  });

  // A suite dolphin would refuse takes every case in it down, and the resulting failure
  // names none of them. Better to say so before the run than after.
  it("refuses to run a suite dolphin would not load", async () => {
    const user = userEvent.setup();
    const { transport, requests } = stubTransport(outcome());
    renderTab(
      transport,
      fakeStore([
        { flow: "orders", content: "flow: orders\ncases:\n  - name: a\n    spys: {}\n" },
      ]),
    );

    const button = await openSuite(user);
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("title", expect.stringContaining("will not load"));

    await user.click(button);
    expect(requests).toHaveLength(0);
  });

  it("refuses to run a suite with no cases", async () => {
    const user = userEvent.setup();
    const { transport } = stubTransport(outcome());
    renderTab(transport, fakeStore([{ flow: "orders", content: "flow: orders\ncases: []\n" }]));

    // `cases: []` is itself a problem dolphin reports, so the button is doubly blocked.
    expect(await openSuite(user)).toBeDisabled();
  });
});

describe("the report and the suite it belongs to", () => {
  // The report is the last RUN's, not the open suite's — it used to be dropped on
  // navigation, back when a run was always one suite and the console could only have meant
  // the open one. Now every result is filed under the suite that produced it, so moving
  // between two suites to compare them no longer destroys what was just run.
  it("keeps the report when another flow is opened, filed under the suite that ran", async () => {
    const user = userEvent.setup();
    const { transport } = stubTransport(outcome());
    renderTab(
      transport,
      fakeStore([
        { flow: "orders", content: suiteYaml },
        { flow: "refunds", content: "flow: refunds\ncases:\n  - name: b\n    expect: {}\n" },
      ]),
      ["orders", "refunds"],
    );

    await user.click(await openSuite(user));
    await waitFor(() =>
      expect(within(toolbar()).getByText("1 passed")).toBeInTheDocument(),
    );

    await user.click(screen.getByRole("button", { name: "refunds" }));

    // The report is still on screen and still filed under orders. The toolbar now speaks
    // for refunds, which was not in that run — so it shows what refunds HAS rather than a
    // tally it did not earn.
    expect(screen.getByText("it runs")).toBeInTheDocument();
    expect(within(toolbar()).getByText("1 case")).toBeInTheDocument();
    expect(within(toolbar()).queryByText("1 passed")).toBeNull();
  });

  // The server is stateless, so two runs would not corrupt each other — they would just
  // produce two reports for a screen that shows one.
  it("does not start a second run while one is in flight", async () => {
    const user = userEvent.setup();
    let release!: (o: TestRunOutcome) => void;
    const pending = new Promise<TestRunOutcome>((resolve) => {
      release = resolve;
    });
    const { transport, requests } = stubTransport(pending);
    renderTab(transport, fakeStore([{ flow: "orders", content: suiteYaml }]));

    const button = await openSuite(user);
    await user.click(button);
    await waitFor(() => expect(screen.getByText("Running…")).toBeInTheDocument());

    await user.click(button);
    expect(requests).toHaveLength(1);

    release(outcome());
    await waitFor(() =>
      expect(within(toolbar()).getByText("1 passed")).toBeInTheDocument(),
    );
  });
});

vi.mock("react-simple-code-editor", () => ({
  default: ({ value }: { value: string }) => <textarea value={value} readOnly />,
}));
