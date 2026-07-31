import { useEffect, type ReactNode } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
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
import { blankDocument, emptyFlow } from "../../model/document";
import type { EditorDocument } from "../../model/document";
import TestingView from "./TestingView";

/** A document with one top-level flow per name. */
function docWithFlows(...names: string[]): EditorDocument {
  return {
    ...blankDocument(),
    flows: names.map((name) => ({ ...emptyFlow(), name })),
  };
}

function fakeStore(files: TestSuiteFile[] = [], canEdit = true) {
  const map = new Map(files.map((f) => [f.flow, f.content]));
  const saved: { flow: string; content: string }[] = [];
  const removed: string[] = [];
  const store: TestSuiteStore = {
    list: async () => [...map].map(([flow, content]) => ({ flow, content })),
    save: async (_id, flow, content) => {
      saved.push({ flow, content });
    },
    remove: async (_id, flow) => {
      removed.push(flow);
    },
    canEdit: () => canEdit,
  };
  return { store, saved, removed };
}

/** Mount the tab against a saved document (an id) unless `draft`. */
function renderView(
  store: TestSuiteStore,
  doc: EditorDocument,
  { draft = false, extra }: { draft?: boolean; extra?: ReactNode } = {},
) {
  function Seed({ children }: { children: ReactNode }) {
    const { dispatch } = useEditorState();
    useEffect(() => {
      dispatch({ type: EditorActionType.LOAD_DOCUMENT, data: { document: doc } });
      if (!draft) {
        dispatch({ type: EditorActionType.SET_INTEGRATION_ID, data: { id: "one" } });
      }
    }, [dispatch]);
    return <>{children}</>;
  }
  return render(
    <EditorStateProvider>
      <Seed>
        {extra}
        <TestSuiteProvider store={store}>
          <TestingView />
        </TestSuiteProvider>
      </Seed>
    </EditorStateProvider>,
  );
}

const suite = (flow: string, ...cases: string[]) =>
  `flow: ${flow}\ncases:\n${cases.map((c) => `  - name: ${c}\n`).join("")}`;

/** The form opens by default, so the raw file is one click away. */
const showYaml = (user: ReturnType<typeof userEvent.setup>) =>
  user.click(screen.getByRole("button", { name: "YAML" }));

const yamlBox = () => screen.getByRole("textbox") as HTMLTextAreaElement;

describe("TestingView", () => {
  // A suite is a file that has to live somewhere. Offering to author one against a
  // draft would promise a committed artifact and deliver a session-scoped one.
  it("explains that a draft has nowhere to keep a suite", async () => {
    const { store } = fakeStore([], false);
    renderView(store, docWithFlows("orders"), { draft: true });

    expect(
      await screen.findByText("Save this integration to test it"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Flows" })).toBeNull();
  });

  it("says there is nothing to test when the document has no flows", async () => {
    const { store } = fakeStore();
    renderView(store, blankDocument());

    expect(await screen.findByText("No flows to test yet")).toBeInTheDocument();
  });

  // Listed by flow, not by suite: a flow with no tests is the thing you most want to
  // see, and a list of files would leave it invisible.
  it("lists every flow, tested or not, with its case count", async () => {
    const { store } = fakeStore([{ flow: "orders", content: suite("orders", "a", "b") }]);
    renderView(store, docWithFlows("orders", "refunds"));

    const rail = await screen.findByRole("navigation", { name: "Flows" });
    expect(within(rail).getByText("orders")).toBeInTheDocument();
    expect(within(rail).getByText("refunds")).toBeInTheDocument();
    expect(within(rail).getByText("2")).toBeInTheDocument();
    // The untested flow offers to start one instead of showing a count.
    expect(
      within(rail).getByRole("button", { name: "Add tests for refunds" }),
    ).toBeInTheDocument();
  });

  it("shows a suite's YAML when its flow is selected", async () => {
    const user = userEvent.setup();
    const { store } = fakeStore([{ flow: "orders", content: suite("orders", "it runs") }]);
    renderView(store, docWithFlows("orders"));

    await user.click(await screen.findByRole("button", { name: "orders" }));
    await showYaml(user);

    expect(screen.getByText("orders_test.yaml")).toBeInTheDocument();
    expect(yamlBox()).toHaveValue(suite("orders", "it runs"));
  });

  it("invites a flow with no suite to start one, and names the file it will become", async () => {
    const user = userEvent.setup();
    const { store } = fakeStore();
    renderView(store, docWithFlows("Order Intake"));

    await user.click(await screen.findByRole("button", { name: "Order Intake" }));

    expect(screen.getByText('No tests for "Order Intake" yet')).toBeInTheDocument();
    expect(screen.getByText("order-intake_test.yaml")).toBeInTheDocument();
  });

  // The scaffold's one case asserts only that the flow completed, so a brand new suite
  // passes — the user sees green before writing an expectation.
  it("seeds a commented starter suite that dolphin would load", async () => {
    const user = userEvent.setup();
    const { store } = fakeStore();
    renderView(store, docWithFlows("orders"));

    await user.click(await screen.findByRole("button", { name: "orders" }));
    await user.click(screen.getByRole("button", { name: "Add tests" }));
    await showYaml(user);

    const written = yamlBox().value;
    expect(written).toContain('flow: "orders"');
    expect(written).toContain("- name: it runs");
    // No problems: the starter is a file dolphin loads as-is. (The comment banner is not
    // one — it belongs to the form view, which this test has left.)
    expect(screen.queryByRole("alert")).toBeNull();
  });

  // These are load errors: dolphin refuses the whole file, so one unnamed case takes
  // every other case in it down. Waiting for a run to say so would be too late.
  it("reports what dolphin would refuse, as you type", async () => {
    const user = userEvent.setup();
    const { store } = fakeStore([
      { flow: "orders", content: "flow: orders\ncases:\n  - name: a\n    spys: {}\n" },
    ]);
    renderView(store, docWithFlows("orders"));

    await user.click(await screen.findByRole("button", { name: "orders" }));

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("dolphin will not load this suite");
    expect(alert).toHaveTextContent(/unknown field "spys"/);
    // And the rail marks the flow, so a problem is visible without opening it.
    const rail = screen.getByRole("navigation", { name: "Flows" });
    expect(within(rail).getByTitle(/1 problem/)).toBeInTheDocument();
  });

  it("writes an edit back to the capability", async () => {
    const user = userEvent.setup();
    const { store } = fakeStore([{ flow: "orders", content: suite("orders", "a") }]);
    renderView(store, docWithFlows("orders"));

    await user.click(await screen.findByRole("button", { name: "orders" }));
    await showYaml(user);
    await user.type(yamlBox(), "#");

    expect(yamlBox().value).toContain("#");
  });

  it("deletes a suite", async () => {
    const user = userEvent.setup();
    const { store, removed } = fakeStore([{ flow: "orders", content: suite("orders", "a") }]);
    renderView(store, docWithFlows("orders"));

    await user.click(await screen.findByRole("button", { name: "orders" }));
    await user.click(screen.getByRole("button", { name: "Delete tests for orders" }));

    expect(removed).toEqual(["orders"]);
    expect(await screen.findByText('No tests for "orders" yet')).toBeInTheDocument();
  });

  // A flow renamed on the canvas is a different row, but its suite file is still in the
  // store under the old name — so without a guard the tab would go on showing a suite
  // for a flow that no longer exists. Driven through a dispatch rather than a remount,
  // because a remount would clear the selection anyway and prove nothing.
  it("drops a selection whose flow is no longer in the document", async () => {
    const user = userEvent.setup();
    const { store } = fakeStore([{ flow: "orders", content: suite("orders", "a") }]);
    renderView(store, docWithFlows("orders", "refunds"), {
      // Renames `orders` to `checkout` in place, as editing the name on the canvas does.
      extra: <RenameFlow from="orders" to="checkout" />,
    });

    await user.click(await screen.findByRole("button", { name: "orders" }));
    expect(screen.getByText("orders_test.yaml")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "rename" }));

    expect(screen.queryByText("orders_test.yaml")).toBeNull();
    expect(screen.getByText("Pick a flow")).toBeInTheDocument();
  });
});

/** A control that renames a top-level flow, the way the canvas does. */
function RenameFlow({ from, to }: { from: string; to: string }) {
  const { state, dispatch } = useEditorState();
  return (
    <button
      type="button"
      onClick={() =>
        dispatch({
          type: EditorActionType.LOAD_DOCUMENT,
          data: {
            document: {
              ...state.document,
              flows: state.document.flows.map((f) =>
                f.name === from ? { ...f, name: to } : f,
              ),
            },
          },
        })
      }
    >
      rename
    </button>
  );
}

// Guards the belt-and-braces return in TestingView: EditorBody only reaches it through
// the toggle, which hides the tab, but a host wiring it directly must not crash.
describe("TestingView without the capability", () => {
  it("renders nothing", () => {
    const { container } = render(
      <EditorStateProvider>
        <TestingView />
      </EditorStateProvider>,
    );
    expect(container).toBeEmptyDOMElement();
  });
});

vi.mock("react-simple-code-editor", () => ({
  // The real editor is a controlled textarea with a highlight layer; the layer needs a
  // DOM measurement jsdom will not do. A plain textarea keeps the value round-tripping.
  default: ({
    value,
    onValueChange,
    readOnly,
  }: {
    value: string;
    onValueChange: (v: string) => void;
    readOnly?: boolean;
  }) => (
    <textarea
      value={value}
      readOnly={readOnly}
      onChange={(e) => onValueChange(e.target.value)}
    />
  ),
}));
