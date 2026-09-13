import { BrowserWindow, dialog } from "electron";
import path from "node:path";
import { mainWindow } from "./window";

/**
 * The Settings window: a static page beside the bundle, loaded over file:// with its
 * own preload. It has to be reachable when the editor server has failed to start,
 * which is exactly when a wrong runtime binary needs correcting — so it cannot be a
 * screen that server renders.
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
    // Not resizable in width: the form is a single column.
    minWidth: 620,
    maxWidth: 620,
    minHeight: 420,
    title: "Settings",
    // A panel over the app rather than a second app window.
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
    // No extension filter: the binaries have no extension off Windows.
    properties: ["openFile"] as const,
    buttonLabel: "Use This",
  };
  const result =
    win && !win.isDestroyed()
      ? await dialog.showOpenDialog(win, { ...options, properties: [...options.properties] })
      : await dialog.showOpenDialog({ ...options, properties: [...options.properties] });
  return result.canceled ? null : (result.filePaths[0] ?? null);
}
