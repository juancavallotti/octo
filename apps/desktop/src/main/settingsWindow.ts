import { BrowserWindow, dialog } from "electron";
import path from "node:path";
import { mainWindow } from "./window";

/**
 * The Settings window.
 *
 * A local page in its own window rather than a screen inside the editor, because of
 * what it configures: which runtime binary to use, and whether to check for updates.
 * Both have to be reachable when the editor server has *failed to start* — which is
 * exactly when a wrong binary needs correcting — and the editor is served by that
 * server. A settings screen you can only reach when everything already works is not
 * a settings screen.
 *
 * It follows the splash page's precedent: a static file next to the bundle, loaded
 * over file:// with its own preload. No framework, no build step of its own.
 */

let win: BrowserWindow | null = null;

function settingsFile(): string {
  return path.join(__dirname, "static", "settings.html");
}

/** The window's webContents id, so IPC can tell its calls from the editor's. */
export function settingsWebContentsId(): number | null {
  return win && !win.isDestroyed() ? win.webContents.id : null;
}

export function openSettings(): void {
  if (win && !win.isDestroyed()) {
    win.show();
    win.focus();
    return;
  }

  win = new BrowserWindow({
    width: 620,
    height: 560,
    // Not resizable in width: the form is a single column and a 2000px-wide
    // settings window is nobody's idea of one.
    minWidth: 620,
    maxWidth: 620,
    minHeight: 420,
    title: "Settings",
    // A panel over the app rather than a second app window: no menu bar of its own,
    // and on macOS it hides with the app.
    parent: mainWindow() ?? undefined,
    show: false,
    webPreferences: {
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      preload: path.join(__dirname, "..", "preload", "settings.js"),
    },
  });

  win.once("ready-to-show", () => win?.show());
  win.on("closed", () => {
    win = null;
  });
  void win.loadFile(settingsFile());
}

export function closeSettings(): void {
  win?.close();
}

/** Ask for an executable, from the Settings window so the sheet attaches to it. */
export async function pickBinaryFile(title: string): Promise<string | null> {
  const options = {
    title,
    message: title,
    // No extension filter: a Go binary has none on macOS and Linux, and `.exe` on
    // Windows — a filter would hide the file it is meant to help find.
    properties: ["openFile"] as const,
    buttonLabel: "Use This",
  };
  const result =
    win && !win.isDestroyed()
      ? await dialog.showOpenDialog(win, { ...options, properties: [...options.properties] })
      : await dialog.showOpenDialog({ ...options, properties: [...options.properties] });
  return result.canceled ? null : (result.filePaths[0] ?? null);
}
