import type { EditorCommand, EditorStatus } from "./menuTemplate";
import { mainWindow } from "./window";

/**
 * The editor page's last report about itself, which the menu is drawn from, and the
 * way back: a menu item names a command and the page carries it out. The shell never
 * acts on the editor's state itself — it only shows it.
 */

let status: EditorStatus | null = null;

export function editorStatus(): EditorStatus | null {
  return status;
}

const VIEWS = new Set(["flows", "resources", "testing"]);
const FLOWS_VIEWS = new Set(["canvas", "yaml"]);
const FLAGS = [
  "hasResources",
  "hasTests",
  "editingText",
  "canUndo",
  "canRedo",
  "canSave",
  "canRun",
  "canRunFlow",
  "canStop",
  "canvas",
] as const;

/**
 * Accept a report from the page, or refuse one that is not the shape the menu reads.
 * Returns whether it changed anything, so the caller rebuilds the menu only then.
 * A page that stops reporting (navigating, reloading) sends null.
 */
export function setEditorStatus(raw: unknown): boolean {
  const next = raw === null ? null : parse(raw);
  if (raw !== null && next === null) return false;
  if (JSON.stringify(next) === JSON.stringify(status)) return false;
  status = next;
  return true;
}

function parse(raw: unknown): EditorStatus | null {
  if (typeof raw !== "object" || raw === null) return null;
  const r = raw as Record<string, unknown>;
  if (!VIEWS.has(r.view as string) || !FLOWS_VIEWS.has(r.flowsView as string)) return null;
  if (r.runs !== "integration" && r.runs !== "tests") return null;
  if (!FLAGS.every((f) => typeof r[f] === "boolean")) return null;
  const p = r.panels as Record<string, unknown> | null;
  const panels =
    p && typeof p === "object"
      ? {
          components: p.components === true,
          settings: p.settings === true,
          console: p.console === true,
        }
      : null;
  const flags = Object.fromEntries(FLAGS.map((f) => [f, r[f] as boolean])) as Record<
    (typeof FLAGS)[number],
    boolean
  >;
  return {
    ...flags,
    view: r.view as EditorStatus["view"],
    flowsView: r.flowsView as EditorStatus["flowsView"],
    runs: r.runs,
    panels,
  };
}

/** Ask the open editor page to carry out a command. */
export function sendCommand(command: EditorCommand): void {
  mainWindow()?.webContents.send("octo:menu:command", command);
}
