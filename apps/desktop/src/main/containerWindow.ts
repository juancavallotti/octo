import { BrowserWindow, dialog } from "electron";
import path from "node:path";
import { mainWindow } from "./window";

/**
 * The Container window: a static page beside the bundle with its own preload, like
 * Settings. It is the shell's, not the editor's — packaging the folder is something
 * the app does to the folder, and the editor page has no part in it.
 */

let win: BrowserWindow | null = null;
let closedListener: (() => void) | null = null;

/** Be told when the window closes, to stop what it was watching. */
export function onContainerClosed(listener: () => void): void {
  closedListener = listener;
}

/** Which part of the window a menu item asked for. */
export type ContainerFocus = "build" | "push" | "export";

/** The window's webContents id, so IPC can tell its calls from every other page's. */
export function containerWebContentsId(): number | null {
  return win && !win.isDestroyed() ? win.webContents.id : null;
}

export function openContainer(focus: ContainerFocus = "build"): void {
  if (win && !win.isDestroyed()) {
    win.webContents.send("octo:container:focus", focus);
    win.show();
    win.focus();
    return;
  }

  win = new BrowserWindow({
    width: 760,
    height: 720,
    minWidth: 620,
    minHeight: 480,
    title: "Containers",
    parent: mainWindow() ?? undefined,
    show: false,
    webPreferences: {
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      preload: path.join(__dirname, "..", "preload", "container.js"),
    },
  });

  win.once("ready-to-show", () => win?.show());
  win.on("closed", () => {
    win = null;
    closedListener?.();
  });
  void win.loadFile(path.join(__dirname, "static", "container.html"), { query: { focus } });
}

/** Push to the window, when it is open. */
export function toContainer(channel: string, payload: unknown): void {
  if (win && !win.isDestroyed()) win.webContents.send(channel, payload);
}

/** A dialog attached to the window, so it reads as the window's question. */
export function containerDialog(): BrowserWindow | undefined {
  return win && !win.isDestroyed() ? win : undefined;
}

/** Ask where an exported build folder goes. */
export async function pickExportFolder(defaultName: string): Promise<string | null> {
  const parent = containerDialog();
  const options = {
    title: "Export Dockerfile",
    defaultPath: defaultName,
    buttonLabel: "Export",
    message: "The folder is created with a Dockerfile and the integration's files in it.",
    properties: ["createDirectory" as const, "showOverwriteConfirmation" as const],
  };
  const result = parent
    ? await dialog.showSaveDialog(parent, options)
    : await dialog.showSaveDialog(options);
  return result.canceled || !result.filePath ? null : result.filePath;
}
