import { BrowserWindow } from "electron";
import path from "node:path";
import { current } from "./server";
import { confineTo, mainWindow } from "./window";

/**
 * The Platform window: the editor server's own /platform page, so it is drawn with
 * the editor's components, theme and fonts, and talks to the shell through the same
 * bridge. Separate from the editor window so a deploy and its logs can stay on show
 * while the folder is edited.
 */

/** Which part of the window something asked for. */
export type PlatformFocus = "integrations" | "folder" | "versions" | "deployments" | "logs" | "settings";

const FOCUSES = new Set<PlatformFocus>(["integrations", "folder", "versions", "deployments", "logs", "settings"]);

export function isPlatformFocus(v: unknown): v is PlatformFocus {
  return FOCUSES.has(v as PlatformFocus);
}

let win: BrowserWindow | null = null;
let closedListener: (() => void) | null = null;

export function onPlatformClosed(listener: () => void): void {
  closedListener = listener;
}

export function platformWindow(): BrowserWindow | null {
  return win && !win.isDestroyed() ? win : null;
}

function pageUrl(origin: string, focus: PlatformFocus): string {
  return `${origin}/platform?focus=${focus}`;
}

export function openPlatform(focus: PlatformFocus = "integrations"): void {
  const server = current();
  if (!server) return;
  const open = platformWindow();
  if (open) {
    open.webContents.send("octo:paas:focus", focus);
    open.show();
    open.focus();
    return;
  }

  win = new BrowserWindow({
    width: 1100,
    height: 760,
    minWidth: 760,
    minHeight: 520,
    title: "Platform",
    show: false,
    titleBarStyle: process.platform === "darwin" ? "hiddenInset" : "default",
    backgroundColor: "#fafaf9",
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      // The editor's preload: the page is the editor server's, and so is its bridge.
      preload: path.join(__dirname, "..", "preload", "index.js"),
    },
  });
  confineTo(win, server.url);
  win.once("ready-to-show", () => win?.show());
  win.on("closed", () => {
    win = null;
    closedListener?.();
  });
  void win.loadURL(pageUrl(server.url, focus));
}

/**
 * Follow the editor server to a new folder. The page is the server's, so a restart
 * takes it down with the editor; it comes back on the same part it was showing.
 */
export function followServer(origin: string): void {
  const open = platformWindow();
  if (!open) return;
  const shown = new URL(open.webContents.getURL() || pageUrl(origin, "integrations")).searchParams.get("focus");
  confineTo(open, origin);
  void open.loadURL(pageUrl(origin, isPlatformFocus(shown) ? shown : "integrations"));
}

/** The window the user is looking at, to attach a dialog to. */
export function frontWindow(): BrowserWindow | undefined {
  const p = platformWindow();
  if (p?.isFocused()) return p;
  return mainWindow() ?? p ?? undefined;
}
