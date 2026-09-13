import { app, ipcMain, type IpcMainInvokeEvent } from "electron";
import path from "node:path";
import { copyMcpUrl, mcpUrl, revealVault } from "./menu";
import { sameOrigin } from "./origin";
import { current } from "./server";
import { binaryStatus, settings, updateSettings } from "./settings";
import {
  closeSettings,
  pickBinaryFile,
  settingsWebContentsId,
} from "./settingsWindow";
import { canUpdate, checkForUpdates } from "./update";
import { mainWindow } from "./window";
import { openVault, pickVault, recents, reopenCurrent, switchTo } from "./vault";

/**
 * The main-process half of the preload bridge. Every handler is guarded on its
 * sender, because these channels move the app between folders on the filesystem and
 * choose which executable it runs.
 */

/** Reject calls from any frame that is not the editor page we loaded. */
function fromEditor(event: IpcMainInvokeEvent): boolean {
  const origin = current()?.url;
  if (!origin) return false;
  const url = event.senderFrame?.url ?? "";
  return sameOrigin(url, origin);
}

/**
 * Reject calls from anything but the Settings window. Identified by webContents id,
 * not origin: the Settings page is loaded over file://, and every file:// page
 * shares one origin.
 */
function fromSettings(event: IpcMainInvokeEvent): boolean {
  const id = settingsWebContentsId();
  return id !== null && event.sender.id === id;
}

type Handler = (event: IpcMainInvokeEvent, ...args: unknown[]) => unknown;

function guarded(channel: string, allow: (e: IpcMainInvokeEvent) => boolean, fn: Handler): void {
  ipcMain.handle(channel, (event, ...args) => {
    if (!allow(event)) throw new Error(`${channel}: refused, unexpected sender`);
    return fn(event, ...args);
  });
}

function handle(channel: string, fn: Handler): void {
  guarded(channel, fromEditor, fn);
}

function handleSettings(channel: string, fn: Handler): void {
  guarded(channel, fromSettings, fn);
}

/**
 * The editor preferences the page reads, with every default applied here: the stored
 * file may predate a preference. `autoLearn` defaults to false — running the user's
 * flows unasked is opt-in.
 */
function editorPrefs() {
  return { autoLearn: settings().editor?.autoLearn === true };
}

/**
 * Tell the open editor its preferences changed. Pushed rather than polled, because
 * both windows are open at once and a change in Settings takes effect in the window
 * behind it without a reload.
 */
function publishPrefs(): void {
  mainWindow()?.webContents.send("octo:prefs:changed", editorPrefs());
}

/** The Settings window's whole view of the world, rebuilt after every change. */
async function settingsView() {
  return {
    binaries: await Promise.all([binaryStatus("octo"), binaryStatus("dolphin")]),
    autoUpdateCheck: settings().autoUpdateCheck !== false,
    autoLearn: editorPrefs().autoLearn,
    appVersion: app.getVersion(),
    canUpdate: canUpdate(),
  };
}

/**
 * Record a runtime override and restart the server onto it. The restart is not
 * awaited: it can show a modal of its own, and the Settings window repaints with the
 * new binary immediately.
 */
async function setRuntime(name: "octo" | "dolphin", file: string | null) {
  const runtime = { ...settings().runtime };
  if (file) runtime[name] = file;
  else delete runtime[name];
  updateSettings({ runtime });
  void reopenCurrent();
  return settingsView();
}

export function registerIpc(): void {
  handle("octo:mcp:url", () => mcpUrl());
  handle("octo:mcp:copy", () => copyMcpUrl());
  handle("octo:vault:get", () => openVault());
  handle("octo:vault:recents", () =>
    recents().map((v) => ({ path: v.path, name: path.basename(v.path) })),
  );
  // Read-only, and the only settings channel the editor page may touch.
  handle("octo:prefs:get", () => editorPrefs());
  handle("octo:vault:pick", () => pickVault());
  handle("octo:vault:reveal", () => revealVault());
  handle("octo:vault:switch", (_event, target) => {
    // Only a folder the shell already knows about: an arbitrary path would point
    // the server anywhere on the machine with no dialog.
    if (typeof target !== "string") return false;
    const known = recents().some((v) => v.path === target);
    return known ? switchTo(target) : false;
  });

  handleSettings("octo:settings:get", () => settingsView());
  handleSettings("octo:settings:setBinary", (_event, name, file) => {
    if (name !== "octo" && name !== "dolphin") throw new Error("unknown binary");
    if (file !== null && typeof file !== "string") throw new Error("bad path");
    return setRuntime(name, file);
  });
  handleSettings("octo:settings:pickBinary", async (_event, name) => {
    if (name !== "octo" && name !== "dolphin") throw new Error("unknown binary");
    const chosen = await pickBinaryFile(`Choose the ${name} binary`);
    // Cancelling is not "use the bundled one", so nothing changes.
    return chosen ? setRuntime(name, chosen) : settingsView();
  });
  handleSettings("octo:settings:autoUpdate", (_event, enabled) => {
    updateSettings({ autoUpdateCheck: enabled !== false });
    return settingsView();
  });
  handleSettings("octo:settings:autoLearn", (_event, enabled) => {
    updateSettings({ editor: { autoLearn: enabled === true } });
    publishPrefs();
    return settingsView();
  });
  handleSettings("octo:settings:checkUpdate", () => checkForUpdates(false));
  handleSettings("octo:settings:close", () => closeSettings());
}
