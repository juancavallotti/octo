import { describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorStateProvider } from "../state/editorState";
import { LayoutProvider } from "../state/layout";
import { ConsoleProvider } from "../run/console";
import { CanvasZoomProvider, useCanvasZoom } from "../canvas/ZoomContext";
import { TestSuiteProvider, type TestSuiteStore } from "../providers/TestSuiteProvider";
import { isEditorCommand, useEditorCommands, type EditorCommand } from "./useEditorCommands";

/**
 * The command runner, taken without a click: clicking a button would move focus out of
 * the field the undo test depends on.
 */
let last: ((c: EditorCommand) => void) | null = null;
function captured() {
  if (!last) throw new Error("no command runner captured");
  return { run: last };
}

const tests = { list: async () => [], canEdit: () => true } as unknown as TestSuiteStore;

/** One button per command, and the status printed back, as a host's menu would see it. */
function Host({ commands }: { commands: EditorCommand[] }) {
  const { run, status } = useEditorCommands();
  const zoom = useCanvasZoom();
  last = run;
  return (
    <>
      {commands.map((c) => (
        <button key={c} onClick={() => run(c)}>
          {c}
        </button>
      ))}
      <input aria-label="field" />
      <pre data-testid="status">{JSON.stringify(status)}</pre>
      <p data-testid="zoom">{zoom.zoom}</p>
    </>
  );
}

function renderHost(commands: EditorCommand[], { withTests = false } = {}) {
  const host = <Host commands={commands} />;
  return render(
    <EditorStateProvider>
      <CanvasZoomProvider>
        <LayoutProvider>
          <ConsoleProvider>
            {withTests ? <TestSuiteProvider store={tests}>{host}</TestSuiteProvider> : host}
          </ConsoleProvider>
        </LayoutProvider>
      </CanvasZoomProvider>
    </EditorStateProvider>,
  );
}

const status = () => JSON.parse(screen.getByTestId("status").textContent ?? "{}");

describe("useEditorCommands", () => {
  it("reports the flows on the canvas, with nothing to undo and no runner", () => {
    renderHost([]);
    expect(status()).toMatchObject({
      view: "flows",
      flowsView: "canvas",
      canvas: true,
      canUndo: false,
      canRun: false,
      runs: "integration",
      editingText: false,
      panels: { components: true, settings: true, console: false },
    });
  });

  it("switches views and how the flows show", async () => {
    const user = userEvent.setup();
    renderHost(["view:testing", "flows:yaml"], { withTests: true });
    await user.click(screen.getByText("view:testing"));
    expect(status()).toMatchObject({ view: "testing", runs: "tests", canvas: false });
    await user.click(screen.getByText("flows:yaml"));
    expect(status()).toMatchObject({ view: "flows", flowsView: "yaml", canvas: false });
  });

  // A host may forward a command blindly; one the editor cannot honour does nothing.
  it("ignores a view the host gave it no store for", async () => {
    const user = userEvent.setup();
    renderHost(["view:resources"]);
    await user.click(screen.getByText("view:resources"));
    expect(status().view).toBe("flows");
  });

  it("makes a new flow, which undo then takes back", async () => {
    const user = userEvent.setup();
    renderHost(["new:flow", "undo"]);
    await user.click(screen.getByText("new:flow"));
    expect(status().canUndo).toBe(true);
    await user.click(screen.getByText("undo"));
    expect(status()).toMatchObject({ canUndo: false, canRedo: true });
  });

  // In a text field, undo is the field's own: the document's history is left alone.
  it("undoes typing, not the document, while a field has focus", async () => {
    const user = userEvent.setup();
    const exec = vi.fn(() => true);
    document.execCommand = exec;
    renderHost(["new:flow"]);
    await user.click(screen.getByText("new:flow"));
    const { run } = captured();
    act(() => screen.getByLabelText("field").focus());
    expect(status().editingText).toBe(true);
    run("undo");
    expect(exec).toHaveBeenCalledWith("undo");
    expect(status().canUndo).toBe(true);
  });

  it("toggles panels and zooms the canvas", async () => {
    const user = userEvent.setup();
    renderHost(["toggle:components", "toggle:console", "canvas:zoom-in", "canvas:zoom-reset"]);
    await user.click(screen.getByText("toggle:components"));
    await user.click(screen.getByText("toggle:console"));
    expect(status().panels).toMatchObject({ components: false, console: true });
    await user.click(screen.getByText("canvas:zoom-in"));
    expect(Number(screen.getByTestId("zoom").textContent)).toBeGreaterThan(1);
    await user.click(screen.getByText("canvas:zoom-reset"));
    expect(screen.getByTestId("zoom")).toHaveTextContent("1");
  });

  it("recognises its own commands and nothing else", () => {
    expect(isEditorCommand("new:suite")).toBe(true);
    expect(isEditorCommand("new:anything")).toBe(false);
    expect(isEditorCommand(42)).toBe(false);
  });
});
