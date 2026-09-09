import { BrowserWindow, app, shell } from "electron";
import path from "node:path";
import { sameOrigin } from "./origin";

/**
 * The one window, and the rules about what may be shown in it.
 *
 * Two of those rules are load-bearing security properties rather than polish:
 *
 *  - The renderer is sandboxed with context isolation on and node integration
 *    off. It is showing a local web app, and a local web app that can require()
 *    is a local web app that can do anything.
 *  - Navigation is pinned to the server's own origin, compared by parsing rather
 *    than by string prefix (see sameOrigin). The editor links out to the docs, and
 *    without this a docs link would replace the editor with a chromeless browser
 *    that has no back button and no way home. External URLs go to the real
 *    browser, which is where the user's session and extensions already are.
 */

let win: BrowserWindow | null = null;

/** The splash page, shown while the server comes up and across a vault switch. */
export function splashFile(): string {
  // esbuild bundles to dist/main/index.js; the static dir is copied beside it.
  return path.join(__dirname, "static", "splash.html");
}

export function mainWindow(): BrowserWindow | null {
  return win && !win.isDestroyed() ? win : null;
}

export function createWindow(): BrowserWindow {
  win = new BrowserWindow({
    width: 1440,
    height: 900,
    minWidth: 900,
    minHeight: 600,
    show: false,
    title: "Octo",
    // A native-feeling title area that still leaves room for the traffic lights.
    titleBarStyle: process.platform === "darwin" ? "hiddenInset" : "default",
    backgroundColor: "#fafaf9",
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      preload: path.join(__dirname, "..", "preload", "index.js"),
    },
  });

  win.once("ready-to-show", () => win?.show());
  void win.loadFile(splashFile());
  return win;
}

/**
 * Confine the window to one origin and send everything else to the browser.
 * Called once the server URL is known, so the allowed origin is a fact rather
 * than a guess.
 */
export function confineTo(target: BrowserWindow, origin: string): void {
  const external = (url: string) => {
    if (url.startsWith("http:") || url.startsWith("https:")) void shell.openExternal(url);
  };

  target.webContents.setWindowOpenHandler(({ url }) => {
    external(url);
    return { action: "deny" };
  });

  target.webContents.on("will-navigate", (event, url) => {
    // file:// is the splash page, which the app itself loads during a restart.
    if (sameOrigin(url, origin) || url.startsWith("file:")) return;
    event.preventDefault();
    external(url);
  });
}

/** Show a line of progress on the splash page; a no-op once the editor is loaded. */
export async function splashHint(text: string): Promise<void> {
  const target = mainWindow();
  if (!target) return;
  try {
    await target.webContents.executeJavaScript(
      `window.octoSplashHint && window.octoSplashHint(${JSON.stringify(text)})`,
    );
  } catch {
    // The page navigated mid-narration. Nothing to say and nobody to say it to.
  }
}

/** Put the splash back up, for a restart that will take a moment. */
export async function showSplash(): Promise<void> {
  const target = mainWindow();
  if (target) await target.loadFile(splashFile());
}

app.on("window-all-closed", () => {
  // macOS convention: closing the window does not quit the app. Quitting is what
  // stops the server, so this also means a closed window leaves running flows
  // running — which is what a user who closed a window by reflex would want.
  if (process.platform !== "darwin") app.quit();
});
