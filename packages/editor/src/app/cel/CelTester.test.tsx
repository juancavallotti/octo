import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import CelTester from "./CelTester";
import { CelTesterProvider } from "./CelTesterStore";
import { RunProvider } from "../run/RunContext";
import { emptyTotals } from "../run/transport";
import type { RunTransport } from "../run/transport";

// EditorState is read by RunProvider (validation/sync); stub it so the module
// renders in isolation without a real document store.
vi.mock("../state/editorState", () => ({
  useEditorState: () => ({
    state: {
      document: { connectors: [], flows: [], processors: [], env: [] },
      integration: { id: undefined, name: "test" },
    },
    dispatch: () => {},
  }),
  EditorActionType: {},
}));

function stubTransport(overrides?: Partial<RunTransport>): RunTransport {
  return {
    status: async () => ({
      available: true,
      running: false,
      version: null,
      testAvailable: false,
      testVersion: null,
      testUrl: null,
      exposable: false,
      reloadsOnSave: false,
    }),
    start: async () => ({
      available: true,
      running: true,
      version: null,
      testAvailable: false,
      testVersion: null,
      testUrl: null,
      exposable: false,
      reloadsOnSave: false,
    }),
    stop: async () => {},
    sync: async () => {},
    invoke: async () => ({
      ok: true,
      dropped: false,
      timedOut: false,
      output: "",
      logs: [],
    }),
    evalCel: async () => ({ ok: true, result: 3 }),
    subscribeLogs: () => () => {},
    // These fixtures exercise RUN, not the Testing tab: no dolphin configured.
    test: async () => ({
      ok: false,
      timedOut: false,
      totals: emptyTotals(),
      suites: [],
      logs: [],
      error: "no test runner",
    }),
    ...overrides,
  };
}

function renderTester(transport: RunTransport) {
  return render(
    <RunProvider transport={transport}>
      <CelTesterProvider>
        <CelTester />
      </CelTesterProvider>
    </RunProvider>,
  );
}

describe("CelTester", () => {
  it("evaluates the expression and shows the result", async () => {
    const evalCel = vi.fn(async () => ({ ok: true, result: 3 }));
    renderTester(stubTransport({ evalCel }));

    const expr = screen.getByPlaceholderText("CEL expression");
    fireEvent.change(expr, { target: { value: "1 + 2", selectionStart: 5 } });

    const runButton = screen.getByRole("button", { name: /Run/ });
    await waitFor(() => expect(runButton).not.toBeDisabled());
    fireEvent.click(runButton);

    expect(await screen.findByText("3")).toBeInTheDocument();
    expect(evalCel).toHaveBeenCalledWith(
      expect.objectContaining({ expression: "1 + 2" }),
    );
  });

  it("surfaces a CEL evaluation error", async () => {
    renderTester(
      stubTransport({
        evalCel: async () => ({
          ok: false,
          error: "undeclared reference to 'nope'",
        }),
      }),
    );

    fireEvent.change(screen.getByPlaceholderText("CEL expression"), {
      target: { value: "nope", selectionStart: 4 },
    });
    const runButton = screen.getByRole("button", { name: /Run/ });
    await waitFor(() => expect(runButton).not.toBeDisabled());
    fireEvent.click(runButton);

    expect(await screen.findByText(/undeclared reference/)).toBeInTheDocument();
  });

  it("rejects invalid body JSON before calling the runner", async () => {
    const evalCel = vi.fn(async () => ({ ok: true, result: null }));
    renderTester(stubTransport({ evalCel }));

    fireEvent.change(screen.getByPlaceholderText("CEL expression"), {
      target: { value: "body", selectionStart: 4 },
    });
    fireEvent.change(screen.getByPlaceholderText('{ "id": 1 }'), {
      target: { value: "{ not json" },
    });
    const runButton = screen.getByRole("button", { name: /Run/ });
    await waitFor(() => expect(runButton).not.toBeDisabled());
    fireEvent.click(runButton);

    expect(
      await screen.findByText(/body must be valid JSON/),
    ).toBeInTheDocument();
    expect(evalCel).not.toHaveBeenCalled();
  });

  it("keeps Cmd+Enter to itself, so Run does not also start the integration", async () => {
    // The document-level Run shortcut deliberately fires from inside text fields (a
    // Run key that needs focus elsewhere reads as broken). This tab is the one place
    // that claims the same chord, so it is the one place that has to stop the event.
    const evalCel = vi.fn(async () => ({ ok: true, result: null }));
    renderTester(stubTransport({ evalCel }));
    const seen = vi.fn();
    document.addEventListener("keydown", seen);

    const field = screen.getByPlaceholderText("CEL expression");
    fireEvent.change(field, { target: { value: "1 + 1", selectionStart: 5 } });
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /Run/ })).not.toBeDisabled(),
    );
    fireEvent.keyDown(field, { key: "Enter", metaKey: true });

    await waitFor(() => expect(evalCel).toHaveBeenCalled());
    expect(seen).not.toHaveBeenCalled();
    document.removeEventListener("keydown", seen);
  });
});
