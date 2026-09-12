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
 * The main-process half of the preload bridge.
 *
 * Every handler is guarded on the sender's origin. The renderer is showing a page
 * from the local server, and these channels can move the app between folders on
 * the filesystem — so a frame that is not that page has no business calling them.
 * In practice nothing else can be loaded (navigation is pinned in window.ts), and
 * that is exactly why the check is cheap to keep: it costs nothing and it stays
 * true if someone later relaxes the navigation rule. Both guards go through
 * sameOrigin, so neither can drift into the prefix comparison they both started
 * with — which a URL like http://127.0.0.1:8477@evil.example/ satisfies without
 * being served by us at all.
 */

/** Reject calls from any frame that is not the editor page we loaded. */
function fromEditor(event: IpcMainInvokeEvent): boolean {
  const origin = current()?.url;
  if (!origin) return false;
  const url = event.senderFrame?.url ?? "";
  return sameOrigin(url, origin);
}

/**
 * Reject calls from anything but the Settings window.
 *
 * The origin check above cannot serve here: the Settings page is loaded over file://,
 * and every file:// page shares one origin — so an origin comparison would admit any
 * local HTML that got itself loaded. The window's webContents id is the only thing
 * that identifies *this* window, and it is what these channels are worth guarding
 * with: they choose which executable the app runs. The editor page, which renders the
 * user's own flow files, must never reach them.
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
 * The editor preferences the page reads, with every default applied here rather than
 * in the page: the stored file may predate a preference, and the shell is the one that
 * knows what its absence should mean.
 *
 * `autoLearn` defaults to false, unlike `autoUpdateCheck` — the editor running the
 * user's flows by itself is a thing to be asked for, not a thing to be opted out of.
 */
function editorPrefs() {
  return { autoLearn: settings().editor?.autoLearn === true };
}

/**
 * Tell the open editor its preferences changed.
 *
 * Pushed rather than polled because the two windows are both open at once: a checkbox
 * ticked in Settings should take effect in the editor behind it, and asking the user to
 * reload the page for a checkbox would be a strange thing to ask.
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
 * Record a runtime override and restart the server onto it.
 *
 * The restart is deliberately not awaited: it shows a splash, asks about running
 * flows and can take a second or two, and the Settings window should repaint with the
 * new binary immediately rather than sitting frozen behind a modal it did not open.
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
  // Read-only, and the only settings channel the editor page may touch: it says how
  // the editor should behave, not what the shell should execute.
  handle("octo:prefs:get", () => editorPrefs());
  handle("octo:vault:pick", () => pickVault());
  handle("octo:vault:reveal", () => revealVault());
  handle("octo:vault:switch", (_event, target) => {
    // The renderer may only switch to a folder the shell already knows about.
    // An arbitrary path from the page would let the editor point the server at
    // any directory on the machine without the user ever seeing a dialog.
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
