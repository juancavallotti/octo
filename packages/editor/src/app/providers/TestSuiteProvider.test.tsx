import { useEffect, useState, type ReactNode } from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  EditorStateProvider,
  useEditorState,
  EditorActionType,
} from "../state/editorState";
import {
  TestSuiteProvider,
  useTestSuites,
  type TestSuiteFile,
  type TestSuiteStore,
} from "./TestSuiteProvider";

/** How long the provider waits before writing; one place so a retune moves one number. */
const DEBOUNCE_MS = 2000;
const PAST_DEBOUNCE_MS = DEBOUNCE_MS + 100;

/**
 * A store backed by plain maps, one per document, recording what it was asked to do.
 * Keyed by document so a reload can be told apart from a stale render.
 */
function fakeStore(docs: Record<string, TestSuiteFile[]> = {}) {
  const byDoc = new Map(
    Object.entries(docs).map(([id, files]) => [
      id,
      new Map(files.map((f) => [f.flow, f.content])),
    ]),
  );
  const filesFor = (id: string | null) => {
    const key = id ?? "";
    const found = byDoc.get(key);
    if (found) return found;
    const fresh = new Map<string, string>();
    byDoc.set(key, fresh);
    return fresh;
  };
  const saves: { id: string | null; flow: string; content: string }[] = [];
  const removes: { id: string | null; flow: string }[] = [];
  const store: TestSuiteStore = {
    list: async (id) => [...filesFor(id)].map(([flow, content]) => ({ flow, content })),
    save: async (id, flow, content) => {
      saves.push({ id, flow, content });
      filesFor(id).set(flow, content);
    },
    remove: async (id, flow) => {
      removes.push({ id, flow });
      filesFor(id).delete(flow);
    },
    canEdit: (id) => !!id,
  };
  /** A write by somebody other than this editor — an MCP agent, in practice. */
  const external = (id: string, flow: string, content: string) =>
    filesFor(id).set(flow, content);
  return { store, saves, removes, external };
}

/** Renders the capability's current view and exposes buttons to drive it. */
function Probe() {
  const suites = useTestSuites();
  const { dispatch } = useEditorState();
  if (!suites) return <p>no capability</p>;
  return (
    <div>
      <p data-testid="loaded">{String(suites.loaded)}</p>
      <p data-testid="persist">{String(suites.canPersist)}</p>
      <p data-testid="orders">{suites.suiteFor("orders") ?? "none"}</p>
      <p data-testid="all">
        {suites
          .all()
          .map((s) => s.flow)
          .join(",")}
      </p>
      <button onClick={() => suites.setSuite("orders", "flow: orders\nv2\n")}>edit</button>
      <button onClick={() => void suites.removeSuite("orders")}>delete</button>
      <button
        onClick={() =>
          dispatch({ type: EditorActionType.SET_INTEGRATION_ID, data: { id: "two" } })
        }
      >
        open other
      </button>
    </div>
  );
}

/**
 * Mounts the provider against a document. `integrationId` null is an unsaved draft,
 * which is the editor's initial state and needs no dispatch.
 */
function renderProvider(store: TestSuiteStore | null, integrationId: string | null = "one") {
  // Stands in for the host's event subscription: bumped when something else wrote a
  // suite for the open document.
  let bump = () => {};
  function Seed({ children }: { children: ReactNode }) {
    const { dispatch } = useEditorState();
    const [token, setToken] = useState(0);
    bump = () => act(() => setToken((n) => n + 1));
    useEffect(() => {
      if (integrationId) {
        dispatch({ type: EditorActionType.SET_INTEGRATION_ID, data: { id: integrationId } });
      }
    }, [dispatch]);
    return (
      <TestSuiteProvider store={store} reloadToken={token}>
        {children}
      </TestSuiteProvider>
    );
  }
  render(
    <EditorStateProvider>
      <Seed>
        <Probe />
      </Seed>
    </EditorStateProvider>,
  );
  return { announce: () => bump() };
}

describe("TestSuiteProvider", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  /** Advance past the save debounce and let the writes it fires settle. */
  async function settleDebounce() {
    await act(async () => {
      vi.advanceTimersByTime(PAST_DEBOUNCE_MS);
    });
  }

  // EditorRoot omits the provider entirely when the host backs no store, and that
  // absence is what hides the tab. Mounted with a null store anyway, it must still
  // render rather than throw — it simply reports that nothing can be kept.
  it("reports nothing persistable when mounted without a store", () => {
    render(
      <EditorStateProvider>
        <TestSuiteProvider store={null}>
          <Probe />
        </TestSuiteProvider>
      </EditorStateProvider>,
    );

    expect(screen.getByTestId("persist")).toHaveTextContent("false");
    expect(screen.getByTestId("loaded")).toHaveTextContent("true");
    expect(screen.getByTestId("orders")).toHaveTextContent("none");
  });

  it("loads the suites stored against the open document", async () => {
    const { store } = fakeStore({
      one: [
        { flow: "refunds", content: "flow: refunds\n" },
        { flow: "orders", content: "flow: orders\n" },
      ],
    });
    renderProvider(store);

    // Listed by flow name, not by the order the store handed them back.
    expect(await screen.findByText("orders,refunds")).toBeInTheDocument();
    expect(screen.getByTestId("orders")).toHaveTextContent("flow: orders");
    expect(screen.getByTestId("loaded")).toHaveTextContent("true");
    expect(screen.getByTestId("persist")).toHaveTextContent("true");
  });

  // An edit must be visible at once — the tab renders from this — while the write is
  // debounced, so typing in the YAML view does not write a file per keystroke.
  it("shows an edit immediately and writes it once the pause is over", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const { store, saves } = fakeStore({ one: [{ flow: "orders", content: "flow: orders\n" }] });
    renderProvider(store);
    await screen.findByText("orders");

    await user.click(screen.getByRole("button", { name: "edit" }));

    expect(screen.getByTestId("orders")).toHaveTextContent("v2");
    expect(saves).toEqual([]);

    await settleDebounce();

    expect(saves).toEqual([{ id: "one", flow: "orders", content: "flow: orders\nv2\n" }]);
  });

  // Writing every suite on every debounce would churn the mtime of files a terminal
  // may be watching, and rewrite content the user never touched.
  it("writes only the suites that changed", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const { store, saves } = fakeStore({
      one: [
        { flow: "orders", content: "flow: orders\n" },
        { flow: "refunds", content: "flow: refunds\n" },
      ],
    });
    renderProvider(store);
    await screen.findByText("orders,refunds");

    await user.click(screen.getByRole("button", { name: "edit" }));
    await settleDebounce();

    expect(saves.map((s) => s.flow)).toEqual(["orders"]);
  });

  // A deletion is not something to leave pending: the user asked for the file to go.
  it("deletes a suite immediately rather than on the debounce", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const { store, removes } = fakeStore({ one: [{ flow: "orders", content: "x" }] });
    renderProvider(store);
    await screen.findByText("orders");

    await user.click(screen.getByRole("button", { name: "delete" }));

    expect(removes).toEqual([{ id: "one", flow: "orders" }]);
    expect(screen.getByTestId("orders")).toHaveTextContent("none");
  });

  // Edit, then delete before the debounce fires. Letting the queued write land after
  // the delete would recreate the file the user just removed.
  it("does not resurrect a suite deleted while its write was pending", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const { store, saves, removes } = fakeStore({ one: [{ flow: "orders", content: "x" }] });
    renderProvider(store);
    await screen.findByText("orders");

    await user.click(screen.getByRole("button", { name: "edit" }));
    await user.click(screen.getByRole("button", { name: "delete" }));
    await settleDebounce();

    expect(removes).toEqual([{ id: "one", flow: "orders" }]);
    expect(saves).toEqual([]);
    expect(screen.getByTestId("orders")).toHaveTextContent("none");
  });

  // A draft has no id to key by. Authoring still works — it would be strange to refuse
  // typing — but nothing is written, and the tab says so via canPersist.
  it("reports a draft as unpersistable and keeps its suites for the session", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const { store, saves } = fakeStore();
    renderProvider(store, null);
    await screen.findByTestId("persist");

    expect(screen.getByTestId("persist")).toHaveTextContent("false");

    await user.click(screen.getByRole("button", { name: "edit" }));
    await settleDebounce();

    expect(screen.getByTestId("orders")).toHaveTextContent("v2");
    expect(saves).toEqual([]);
  });

  // A host may back suites but refuse writes for this document (a read-only view).
  // Same contract as a draft: the content is there, it just does not get written.
  it("respects a store that will not accept writes for this document", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const { store, saves } = fakeStore({ one: [{ flow: "orders", content: "flow: orders\n" }] });
    renderProvider({ ...store, canEdit: () => false });
    await screen.findByText("orders");

    expect(screen.getByTestId("persist")).toHaveTextContent("false");

    await user.click(screen.getByRole("button", { name: "edit" }));
    await settleDebounce();

    expect(screen.getByTestId("orders")).toHaveTextContent("v2");
    expect(saves).toEqual([]);
  });

  // Suites belong to a document, so opening another must not leave the previous one's
  // on screen — they would be attributed to flows that do not exist here.
  it("reloads when another document is opened", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const { store } = fakeStore({
      one: [{ flow: "orders", content: "flow: orders\n" }],
      two: [{ flow: "invoices", content: "flow: invoices\n" }],
    });
    renderProvider(store);
    await screen.findByText("orders");

    await user.click(screen.getByRole("button", { name: "open other" }));

    expect(await screen.findByText("invoices")).toBeInTheDocument();
    expect(screen.getByTestId("orders")).toHaveTextContent("none");
  });

  /**
   * The editor is not the only writer: an MCP agent authors suites through the same
   * store. Without this the tab shows whatever it read on mount until the user
   * navigates away and back — which from their side is indistinguishable from the agent
   * having done nothing.
   */
  it("picks up a suite written elsewhere when the host says so", async () => {
    const { store, external } = fakeStore({ one: [] });
    const { announce } = renderProvider(store);
    await screen.findByTestId("all");

    external("one", "orders", "flow: orders\nfrom the agent\n");
    announce();

    expect(await screen.findByText("orders")).toBeInTheDocument();
    expect(screen.getByTestId("orders")).toHaveTextContent("from the agent");
  });

  // The provider does not poll. Nothing appears until the host relays an event, which
  // is what keeps a list call off the critical path of every render.
  it("shows nothing new until the host says so", async () => {
    const { store, external } = fakeStore({ one: [] });
    renderProvider(store);
    await screen.findByTestId("all");

    external("one", "orders", "flow: orders\n");
    await act(async () => {
      vi.advanceTimersByTime(PAST_DEBOUNCE_MS);
    });

    expect(screen.getByTestId("orders")).toHaveTextContent("none");
  });

  /**
   * A refresh is news from elsewhere, not permission to discard what someone is
   * typing. The pending edit is about to be written anyway, so adopting the stored copy
   * would undo it on screen and then save the undo.
   */
  it("keeps a suite whose edit has not been written yet", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const { store, external } = fakeStore({ one: [{ flow: "orders", content: "v1" }] });
    const { announce } = renderProvider(store);
    await screen.findByText("orders");

    await user.click(screen.getByRole("button", { name: "edit" }));
    external("one", "orders", "written elsewhere");
    announce();
    // Let the re-list land: asserting before it does would pass however the merge
    // behaves, which is the whole thing under test.
    await act(async () => {});

    expect(screen.getByTestId("orders")).toHaveTextContent("v2");
  });

  // Only the suite being edited is held back. Anything else the writer touched is news
  // worth having, and holding all of it would make one open editor freeze the lot.
  it("refreshes the untouched suites even while another is being edited", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    const { store, external } = fakeStore({
      one: [
        { flow: "orders", content: "v1" },
        { flow: "refunds", content: "flow: refunds\n" },
      ],
    });
    const { announce } = renderProvider(store);
    await screen.findByText("orders,refunds");

    await user.click(screen.getByRole("button", { name: "edit" }));
    external("one", "refunds", "flow: refunds\nfrom the agent\n");
    announce();

    await waitFor(() =>
      expect(screen.getByTestId("all")).toHaveTextContent("orders,refunds"),
    );
    expect(screen.getByTestId("orders")).toHaveTextContent("v2");
  });

  // An unreadable store is not worth blocking the editor over; the tab shows an empty
  // document rather than hanging on a load that will never settle.
  it("survives a store that cannot be read", async () => {
    const store: TestSuiteStore = {
      list: async () => {
        throw new Error("nope");
      },
      save: async () => {},
      remove: async () => {},
      canEdit: () => true,
    };
    renderProvider(store);

    expect(await screen.findByText("none")).toBeInTheDocument();
    expect(screen.getByTestId("loaded")).toHaveTextContent("true");
  });
});
