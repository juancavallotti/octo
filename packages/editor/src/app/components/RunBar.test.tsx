import { useEffect, type ReactNode } from "react";
import { describe, it, expect } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  EditorStateProvider,
  useEditorState,
  EditorActionType,
} from "../state/editorState";
import {
  TestSuiteProvider,
  type TestSuiteFile,
  type TestSuiteStore,
} from "../providers/TestSuiteProvider";
import { RunProvider } from "../run/RunContext";
import { SuiteRunProvider } from "../run/SuiteRunContext";
import { ConsoleProvider } from "../run/console";
import { emptyTotals, type TestRunRequest } from "../run/testTransport";
import type { RunTransport } from "../run/transport";
import { blankDocument, emptyFlow } from "../model/document";
import RunBar from "./RunBar";

/**
 * The header RUN control is contextual: on the Testing tab it runs the TESTS, because that
 * is the only kind of run that tab is about, and it runs ALL of them — the per-suite button
 * inside the tab is for running one.
 */

const suiteYaml = (flow: string) =>
  `flow: ${flow}\ncases:\n  - name: it runs\n    expect: {}\n`;

function snapshot(
  over: { running?: boolean; testAvailable?: boolean; reloadsOnSave?: boolean } = {},
) {
  return {
    available: true,
    running: over.running ?? false,
    version: "octo 0.1.0",
    testAvailable: over.testAvailable ?? true,
    testVersion: null,
    testUrl: null,
    exposable: false,
    reloadsOnSave: over.reloadsOnSave ?? false,
  };
}

function stubTransport(
  over: {
    running?: boolean;
    testAvailable?: boolean;
    reloadsOnSave?: boolean;
    hold?: Promise<void>;
  } = {},
) {
  const requests: TestRunRequest[] = [];
  const transport: RunTransport = {
    status: async () => snapshot(over),
    start: async () => snapshot(over),
    stop: async () => {},
    sync: async () => {},
    invoke: async () => ({ ok: true, dropped: false, timedOut: false, output: "", logs: [] }),
    evalCel: async () => ({ ok: true }),
    subscribeLogs: () => () => {},
    test: async (req) => {
      requests.push(req);
      // `hold` keeps a run in flight for as long as the test needs it there.
      if (over.hold) await over.hold;
      return { ok: true, timedOut: false, totals: emptyTotals(), suites: [], logs: [] };
    },
  };
  return { transport, requests };
}

function fakeStore(files: TestSuiteFile[], { canEdit = true } = {}): TestSuiteStore {
  return {
    list: async () => files,
    save: async () => {},
    remove: async () => {},
    canEdit: () => canEdit,
  };
}

function renderBar({
  transport,
  store,
  flows = ["orders"],
  testing = true,
}: {
  transport: RunTransport;
  store: TestSuiteStore;
  flows?: string[];
  testing?: boolean;
}) {
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
      if (testing) {
        dispatch({ type: EditorActionType.SET_VIEW_MODE, data: "testing" });
      }
    }, [dispatch]);
    return <>{children}</>;
  }
  return render(
    <EditorStateProvider>
      <Seed>
        <ConsoleProvider>
          <TestSuiteProvider store={store}>
            <RunProvider transport={transport}>
              <SuiteRunProvider>
                <RunBar />
              </SuiteRunProvider>
            </RunProvider>
          </TestSuiteProvider>
        </ConsoleProvider>
      </Seed>
    </EditorStateProvider>,
  );
}

const runTests = () => screen.getByRole("button", { name: /Run tests/ });

describe("RunBar on the Testing tab", () => {
  it("offers Run tests instead of starting the integration", async () => {
    const { transport } = stubTransport();
    renderBar({ transport, store: fakeStore([{ flow: "orders", content: suiteYaml("orders") }]) });

    expect(await screen.findByRole("button", { name: /Run tests/ })).toBeInTheDocument();
    // Starting the integration is not what that tab is about, so the control is not there
    // to be pressed by accident.
    expect(screen.queryByRole("button", { name: "Run" })).toBeNull();
  });

  it("keeps starting the integration on every other view", async () => {
    const { transport } = stubTransport();
    renderBar({
      transport,
      store: fakeStore([{ flow: "orders", content: suiteYaml("orders") }]),
      testing: false,
    });

    expect(await screen.findByRole("button", { name: "Run" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Run tests/ })).toBeNull();
  });

  // The point of the control: one request carrying every suite, rather than the user
  // walking the rail and running them one at a time.
  it("sends every runnable suite in one request", async () => {
    const user = userEvent.setup();
    const { transport, requests } = stubTransport();
    renderBar({
      transport,
      store: fakeStore([
        { flow: "checkout", content: suiteYaml("checkout") },
        { flow: "orders", content: suiteYaml("orders") },
      ]),
      flows: ["checkout", "orders"],
    });

    await waitFor(() => expect(runTests()).toBeEnabled());
    await user.click(runTests());

    await waitFor(() => expect(requests).toHaveLength(1));
    expect(requests[0].suites.map((s) => s.name)).toEqual(["checkout", "orders"]);
  });

  // dolphin loads every named suite before running any case, so sending one it refuses
  // would abort the whole run and report nothing about the suites that were fine.
  it("leaves out a suite dolphin would refuse, and says which", async () => {
    const user = userEvent.setup();
    const { transport, requests } = stubTransport();
    renderBar({
      transport,
      store: fakeStore([
        { flow: "orders", content: suiteYaml("orders") },
        { flow: "refunds", content: "flow: refunds\ncases:\n  - name: a\n    spys: {}\n" },
      ]),
      flows: ["orders", "refunds"],
    });

    await waitFor(() => expect(runTests()).toBeEnabled());
    expect(runTests()).toHaveAttribute("title", expect.stringContaining("1 not run"));
    expect(runTests()).toHaveAttribute("title", expect.stringContaining("refunds"));

    await user.click(runTests());

    await waitFor(() => expect(requests).toHaveLength(1));
    expect(requests[0].suites.map((s) => s.name)).toEqual(["orders"]);
  });

  // Visible and explaining itself, rather than gone: someone who has just written tests
  // needs to be able to learn why they cannot run them.
  it("stays put and names the missing binary when there is no runner", async () => {
    const user = userEvent.setup();
    const { transport, requests } = stubTransport({ testAvailable: false });
    renderBar({ transport, store: fakeStore([{ flow: "orders", content: suiteYaml("orders") }]) });

    await waitFor(() => expect(runTests()).toBeDisabled());
    expect(runTests()).toHaveAttribute(
      "title",
      expect.stringContaining("DOLPHIN_BIN_PATH"),
    );

    await user.click(runTests());
    expect(requests).toHaveLength(0);
  });

  it("says there is nothing to run when no suites are stored", async () => {
    const { transport } = stubTransport();
    renderBar({ transport, store: fakeStore([]) });

    await waitFor(() => expect(runTests()).toBeDisabled());
    expect(runTests()).toHaveAttribute("title", expect.stringContaining("No test suites yet"));
  });

  // Every suite held back is a different message from having none at all: there is work
  // here, and it is the files that are wrong.
  it("says why when suites are stored but none can run", async () => {
    const { transport } = stubTransport();
    renderBar({
      transport,
      store: fakeStore([{ flow: "orders", content: "flow: orders\ncases: []\n" }]),
    });

    await waitFor(() => expect(runTests()).toBeDisabled());
    expect(runTests()).toHaveAttribute("title", expect.stringContaining("Nothing can run"));
    expect(runTests()).toHaveAttribute("title", expect.stringContaining("has no cases yet"));
  });

  // A suite whose flow was renamed away is invisible in the tab, which lists flows rather
  // than files — but it is still on disk, and CI still runs it.
  it("holds back a suite for a flow the document no longer has", async () => {
    const user = userEvent.setup();
    const { transport, requests } = stubTransport();
    renderBar({
      transport,
      store: fakeStore([
        { flow: "orders", content: suiteYaml("orders") },
        { flow: "legacy", content: suiteYaml("legacy") },
      ]),
      flows: ["orders"],
    });

    await waitFor(() => expect(runTests()).toBeEnabled());
    await user.click(runTests());

    await waitFor(() => expect(requests).toHaveLength(1));
    expect(requests[0].suites.map((s) => s.name)).toEqual(["orders"]);
  });

  // Two runs would not corrupt each other — the host is stateless — but they would produce
  // two reports for a console that shows one.
  it("cannot be pressed while a run is in flight", async () => {
    const user = userEvent.setup();
    let release!: () => void;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    const { transport, requests } = stubTransport({ hold: pending });
    renderBar({ transport, store: fakeStore([{ flow: "orders", content: suiteYaml("orders") }]) });

    await waitFor(() => expect(runTests()).toBeEnabled());
    await user.click(runTests());

    const button = screen.getByRole("button", { name: /Running…/ });
    expect(button).toBeDisabled();
    await user.click(button);

    expect(requests).toHaveLength(1);
    release();
    await waitFor(() => expect(runTests()).toBeEnabled());
  });
});
