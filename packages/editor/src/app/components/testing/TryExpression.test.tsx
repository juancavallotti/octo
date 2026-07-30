import { useEffect, type ReactNode } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  EditorStateProvider,
  useEditorState,
  EditorActionType,
} from "../../state/editorState";
import { ConsoleProvider } from "../../run/console";
import { RunProvider } from "../../run/RunContext";
import { SuiteRunProvider } from "../../run/SuiteRunContext";
import { emptyTotals, type TestRunOutcome } from "../../run/testTransport";
import type { CelEvalRequest, CelEvalResult, RunTransport } from "../../run/transport";
import {
  TestSuiteProvider,
  type TestSuiteStore,
} from "../../providers/TestSuiteProvider";
import { blankDocument, emptyFlow } from "../../model/document";
import TestingView from "./TestingView";

/**
 * Trying a `that:` expression without running the suite.
 *
 * A `that:` entry is the only part of a case that can be wrong in a way nothing else
 * catches until a run — the JSON fields are checked as they are typed and the rules check
 * the rest of the file, but an expression is opaque until something compiles it.
 */

const SNAPSHOT = {
  available: true,
  running: false,
  version: null,
  testAvailable: true,
  testVersion: null,
  testPath: null,
};

/** A report in which "it runs" produced a message, as a real run would return. */
const REPORT: TestRunOutcome = {
  ok: true,
  timedOut: false,
  totals: { ...emptyTotals(), cases: 1, passed: 1 },
  suites: [
    {
      name: "orders",
      flow: "orders",
      cases: [
        {
          name: "it runs",
          status: "passed",
          elapsedMs: 2,
          outcome: {
            result: {
              event_id: "e1",
              variables: { status: "200" },
              body: { total: 42 },
            },
          },
        },
      ],
    },
  ],
  logs: [],
};

function harness(
  evalResult: CelEvalResult,
  { available = true }: { available?: boolean } = {},
) {
  const asked: CelEvalRequest[] = [];
  const transport: RunTransport = {
    status: async () => ({ ...SNAPSHOT, available }),
    start: async () => SNAPSHOT,
    stop: async () => {},
    sync: async () => {},
    invoke: async () => ({ ok: true, dropped: false, timedOut: false, output: "", logs: [] }),
    evalCel: async (req) => {
      asked.push(req);
      return evalResult;
    },
    subscribeLogs: () => () => {},
    test: async () => REPORT,
  };
  return { transport, asked };
}

const store: TestSuiteStore = {
  list: async () => [
    {
      flow: "orders",
      content:
        "flow: orders\ncases:\n  - name: it runs\n    expect:\n      that: ['body.total > 0']\n",
    },
  ],
  save: async () => {},
  remove: async () => {},
  canEdit: () => true,
};

async function openCase(transport: RunTransport) {
  const user = userEvent.setup();

  function Seed({ children }: { children: ReactNode }) {
    const { dispatch } = useEditorState();
    useEffect(() => {
      dispatch({
        type: EditorActionType.LOAD_DOCUMENT,
        data: {
          document: { ...blankDocument(), flows: [{ ...emptyFlow(), name: "orders" }] },
        },
      });
      dispatch({ type: EditorActionType.SET_INTEGRATION_ID, data: { id: "one" } });
    }, [dispatch]);
    return <>{children}</>;
  }

  render(
    <EditorStateProvider>
      <ConsoleProvider>
        <RunProvider transport={transport}>
          <SuiteRunProvider>
            <Seed>
              <TestSuiteProvider store={store}>
                <TestingView />
              </TestSuiteProvider>
            </Seed>
          </SuiteRunProvider>
        </RunProvider>
      </ConsoleProvider>
    </EditorStateProvider>,
  );

  await user.click(await screen.findByRole("button", { name: "orders" }));
  await user.click(screen.getByText("it runs"));
  return user;
}

describe("TryExpression", () => {
  it("evaluates the expression and shows the verdict", async () => {
    const { transport, asked } = harness({ ok: true, result: true });
    const user = await openCase(transport);

    await user.click(screen.getByRole("button", { name: "Try" }));

    expect(asked[0].expression).toBe("body.total > 0");
    expect(await screen.findByText(/^true/)).toBeInTheDocument();
  });

  // `body.total` is not an assertion, and dolphin fails the case rather than reading it
  // as one — a truthy non-zero number quietly passing would be a test that checks
  // nothing. So it is reported as the mistake it is, not as a result.
  it("calls a non-boolean result a mistake, not a false", async () => {
    const { transport } = harness({ ok: true, result: 42 });
    const user = await openCase(transport);

    await user.click(screen.getByRole("button", { name: "Try" }));

    expect(
      await screen.findByText(/Not a true\/false expression/),
    ).toBeInTheDocument();
    expect(screen.queryByText("false")).toBeNull();
  });

  it("shows a compile error", async () => {
    const { transport } = harness({ ok: true, error: "undeclared reference to 'bdy'" });
    const user = await openCase(transport);

    await user.click(screen.getByRole("button", { name: "Try" }));

    expect(await screen.findByText(/undeclared reference/)).toBeInTheDocument();
  });

  // dolphin never loads the config — octo does, in the child process — so a `that:`
  // expression referencing env.X fails there. Binding it here would let the editor bless
  // an expression the run refuses.
  it("does not bind env, because dolphin does not", async () => {
    const { transport, asked } = harness({ ok: true, result: true });
    const user = await openCase(transport);

    await user.click(screen.getByRole("button", { name: "Try" }));

    expect(asked[0].env).toBeUndefined();
  });

  it("says it is evaluating against nothing until there has been a run", async () => {
    const { transport, asked } = harness({ ok: true, result: true });
    const user = await openCase(transport);

    expect(screen.getByRole("button", { name: "Try" })).toHaveAttribute(
      "title",
      expect.stringContaining("only checks that it compiles"),
    );
    await user.click(screen.getByRole("button", { name: "Try" }));

    expect(asked[0].data).toBeUndefined();
    expect(asked[0].vars).toBeUndefined();
    expect(await screen.findByText(/against an empty message/)).toBeInTheDocument();
  });

  // The point of the affordance: after a run, the expression is checked against what the
  // flow actually produced, which is what you go looking for when an assertion fails.
  it("uses the message the last run produced for this case", async () => {
    const { transport, asked } = harness({ ok: true, result: true });
    const user = await openCase(transport);

    await user.click(screen.getByRole("button", { name: "Run tests" }));
    await user.click(screen.getByRole("button", { name: "Try" }));

    expect(asked[0].data).toBe(JSON.stringify({ total: 42 }));
    expect(asked[0].vars).toBe(JSON.stringify({ status: "200" }));
    expect(screen.queryByText(/against an empty message/)).toBeNull();
  });

  it("offers nothing to press when there is no runner", async () => {
    const { transport } = harness({ ok: true, result: true }, { available: false });
    await openCase(transport);

    expect(screen.queryByRole("button", { name: "Try" })).toBeNull();
  });
});

vi.mock("react-simple-code-editor", () => ({
  default: ({
    value,
    onValueChange,
    placeholder,
    readOnly,
  }: {
    value: string;
    onValueChange: (v: string) => void;
    placeholder?: string;
    readOnly?: boolean;
  }) => (
    <textarea
      value={value}
      placeholder={placeholder}
      readOnly={readOnly}
      onChange={(e) => onValueChange(e.target.value)}
    />
  ),
}));
