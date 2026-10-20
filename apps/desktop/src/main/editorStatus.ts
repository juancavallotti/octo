import { dialog, type BrowserWindow } from "electron";
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
  "hasTerminal",
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

/** How long to wait for the editor to report that a save landed. */
const SAVE_TIMEOUT_MS = 5000;

/**
 * Save the editor's pending edits, for an operation that takes the folder as it is
 * on disk. Asked of the editor through the command the File menu sends; done when
 * its next report says there is nothing left to save. An editor that cannot save
 * leaves the choice to the user, told by `detail` what going on without them means,
 * rather than silently using the old file.
 */
export async function ensureSaved(parent: BrowserWindow | undefined, detail: string): Promise<boolean> {
  if (!editorStatus()?.canSave) return true;
  sendCommand("save");
  const deadline = Date.now() + SAVE_TIMEOUT_MS;
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 100));
    if (!editorStatus()?.canSave) return true;
  }
  const options = {
    type: "warning" as const,
    message: "The editor's changes could not be saved.",
    detail,
    buttons: ["Cancel", "Continue Without Them"],
    defaultId: 0,
    cancelId: 0,
  };
  const { response } = parent
    ? await dialog.showMessageBox(parent, options)
    : await dialog.showMessageBox(options);
  return response === 1;
}
