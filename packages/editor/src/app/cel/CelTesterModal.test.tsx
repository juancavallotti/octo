import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import CelTesterModal from "./CelTesterModal";
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
      reloadsOnSave: false,
    }),
    start: async () => ({
      available: true,
      running: true,
      version: null,
      testAvailable: false,
      testVersion: null,
      testUrl: null,
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

function renderModal(transport: RunTransport) {
  return render(
    <RunProvider transport={transport}>
      <CelTesterModal onClose={() => {}} />
    </RunProvider>,
  );
}

describe("CelTesterModal", () => {
  it("evaluates the expression and shows the result", async () => {
    const evalCel = vi.fn(async () => ({ ok: true, result: 3 }));
    renderModal(stubTransport({ evalCel }));

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
    renderModal(
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
    renderModal(stubTransport({ evalCel }));

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
});
