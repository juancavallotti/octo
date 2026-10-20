import { Menu, app, clipboard, dialog, shell, type BrowserWindow } from "electron";
import path from "node:path";
import { logPath } from "./log";
import { current } from "./server";
import { openSettings } from "./settingsWindow";
import { openContainer } from "./containerWindow";
import { openPlatform } from "./platformWindow";
import { pushVault } from "./platform/controller";
import { readLink } from "./platform/link";
import { dockerStatus } from "./container/status";
import { checkForUpdates, restartToUpdate, updateStatus } from "./update";
import { openVault, pickVault, recents, switchTo } from "./vault";
import { editorStatus, sendCommand, setEditorStatus } from "./editorStatus";
import { menuTemplate } from "./menuTemplate";
import { mainWindow } from "./window";

/**
 * The application menu, rebuilt from current state on every change rather than
 * mutated in place, so its dynamic parts cannot drift from what they describe. What
 * it shows is menuTemplate's; this gathers the state and the actions it needs.
 */

/** The MCP endpoint, which is the editor's own URL plus /mcp. */
export function mcpUrl(): string | null {
  const server = current();
  return server ? `${server.url}/mcp` : null;
}

export function copyMcpUrl(): void {
  const url = mcpUrl();
  if (url) clipboard.writeText(url);
}

export function revealVault(): void {
  const vault = openVault();
  if (vault) void shell.openPath(vault.path);
}

/**
 * The update entry: one item that either offers the restart a downloaded update is
 * waiting for, or the check that might find one. Two items would mean one of them is
 * always the wrong thing to click.
 */
function updateEntry(): { label: string; click: () => void } {
  const { stage, version } = updateStatus();
  if (stage === "ready" && version) {
    return { label: `Restart to Update to ${version}`, click: restartToUpdate };
  }
  return { label: "Check for Updates…", click: () => void checkForUpdates(false) };
}

/** Zoom the editor page itself; the canvas has its own zoom, which the page owns. */
function pageZoom(direction: "in" | "out" | "reset"): void {
  const contents = mainWindow()?.webContents;
  if (!contents) return;
  contents.zoomLevel = direction === "reset" ? 0 : contents.zoomLevel + (direction === "in" ? 0.5 : -0.5);
}

/** The open folder's integration, read from the folder itself. */
function linkedIntegration(): { integration: string; profile: string } | null {
  const vault = openVault();
  const link = vault ? readLink(vault.path) : null;
  return link ? { integration: link.integrationName, profile: link.profile } : null;
}

/**
 * Push from the menu. Its progress shows on the editor's platform chip; a failure has
 * no window to say it in when the platform's is closed, so it says it here.
 */
function pushToPlatform(): void {
  pushVault().catch((err: unknown) => {
    const message = err instanceof Error ? err.message : String(err);
    if (message === "Cancelled") return;
    const win = mainWindow();
    const options = { type: "error" as const, message: "The push did not go through.", detail: message };
    void (win ? dialog.showMessageBox(win, options) : dialog.showMessageBox(options));
  });
}

export function buildMenu(): void {
  const update = updateEntry();
  const template = menuTemplate(
    {
      isMac: process.platform === "darwin",
      appName: app.name,
      status: editorStatus(),
      recents: recents().map((v) => ({ label: path.basename(v.path), path: v.path })),
      vaultOpen: openVault() !== null,
      mcpAvailable: mcpUrl() !== null,
      update: { label: update.label },
      docker: dockerStatus().state,
      linked: linkedIntegration(),
    },
    {
      command: sendCommand,
      pageZoom,
      pickVault: () => void pickVault(),
      switchVault: (target) => void switchTo(target),
      revealVault,
      copyMcpUrl,
      openSettings,
      openContainer,
      openPlatform,
      pushToPlatform,
      update: update.click,
      openDocs: () => void shell.openExternal("https://octo.juancavallotti.com"),
      showLog: () => shell.showItemInFolder(logPath()),
    },
  );
  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}

/**
 * Forget the editor's report when its page goes away without saying so — a reload,
 * a navigation, a crashed renderer, a closed window — none of which runs the page's
 * own goodbye. The menu then goes back to what it shows before an editor reports,
 * rather than offering a dead page's commands.
 */
export function followEditorPage(win: BrowserWindow): void {
  const forget = () => {
    if (setEditorStatus(null)) buildMenu();
  };
  win.webContents.on("did-start-navigation", (details) => {
    if (details.isMainFrame && !details.isSameDocument) forget();
  });
  win.webContents.on("render-process-gone", forget);
  win.on("closed", forget);
}
