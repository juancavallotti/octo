import { app, ipcMain, type IpcMainEvent, type IpcMainInvokeEvent } from "electron";
import path from "node:path";
import { buildMenu, copyMcpUrl, mcpUrl, revealVault } from "./menu";
import { sameOrigin } from "./origin";
import { current } from "./server";
import { binaryStatus, settings, updateSettings } from "./settings";
import { RUNTIME_BINARIES, isRuntimeBinary, type RuntimeBinary } from "./paths";
import {
  closeSettings,
  pickBinaryFile,
  settingsWebContentsId,
} from "./settingsWindow";
import {
  canUpdate,
  checkForUpdates,
  onUpdateStatus,
  restartToUpdate,
  updateStatus,
  type UpdateStatus,
} from "./update";
import { setEditorStatus } from "./editorStatus";
import * as container from "./container/controller";
import { refreshDocker } from "./container/status";
import { containerWebContentsId, onContainerClosed } from "./containerWindow";
import * as terminal from "./terminal";
import * as paas from "./platform/controller";
import { readLink } from "./platform/link";
import { frontWindow, isPlatformFocus, onPlatformClosed, openPlatform, platformWindow } from "./platformWindow";
import { mainWindow } from "./window";
import { openVault, pickVault, recents, reopenCurrent, switchTo } from "./vault";

/**
 * The main-process half of the preload bridge. Every handler is guarded on its
 * sender, because these channels move the app between folders on the filesystem and
 * choose which executable it runs.
 */

/** Reject calls from any frame that is not the editor page we loaded. */
function fromEditor(event: IpcMainEvent | IpcMainInvokeEvent): boolean {
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

/** Reject calls from anything but the Container window, identified as Settings is. */
function fromContainer(event: IpcMainInvokeEvent): boolean {
  const id = containerWebContentsId();
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

/**
 * A one-way message from the editor: keystrokes and resizes, which are too frequent
 * to round-trip and have no answer. Refused the same way, but silently — there is no
 * caller to throw at.
 */
function listen(channel: string, fn: (event: IpcMainEvent, ...args: unknown[]) => void): void {
  ipcMain.on(channel, (event, ...args) => {
    if (fromEditor(event)) fn(event, ...args);
  });
}

function handleSettings(channel: string, fn: Handler): void {
  guarded(channel, fromSettings, fn);
}

function handleContainer(channel: string, fn: Handler): void {
  guarded(channel, fromContainer, fn);
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

/**
 * Tell the open editor where the update got to, so its restart button can appear the
 * moment one is ready rather than on the next reload. The menu is rebuilt from the
 * same event: both of them show the one state and neither may lag the other.
 */
function publishUpdate(status: UpdateStatus): void {
  mainWindow()?.webContents.send("octo:update:changed", status);
  buildMenu();
}

/** The docker CLI as the Settings window shows it: the choice, and what it answered. */
async function dockerView() {
  const override = settings().docker;
  const status = await refreshDocker();
  return { ...status, ...(override ? { override } : {}) };
}

/** The Settings window's whole view of the world, rebuilt after every change. */
async function settingsView() {
  return {
    binaries: await Promise.all(RUNTIME_BINARIES.map(binaryStatus)),
    docker: await dockerView(),
    autoUpdateCheck: settings().autoUpdateCheck !== false,
    autoLearn: editorPrefs().autoLearn,
    appVersion: app.getVersion(),
    canUpdate: canUpdate(),
  };
}

/**
 * Record a binary override, and restart the server onto it when the server runs it.
 * The restart is not awaited: it can show a modal of its own, and the Settings window
 * repaints with the new binary immediately. octopaas is run by the shell per call, so
 * the next call picks it up and nothing restarts.
 */
async function setRuntime(name: RuntimeBinary, file: string | null) {
  const runtime = { ...settings().runtime };
  if (file) runtime[name] = file;
  else delete runtime[name];
  updateSettings({ runtime });
  if (name !== "octopaas") void reopenCurrent();
  return settingsView();
}

/** A folder as the editor page is told of it: where it is, and the integration it is when linked. */
function vaultRef(dir: string): { path: string; name: string; integration?: { name: string; profile: string } } {
  const link = readLink(dir);
  return {
    path: dir,
    name: path.basename(dir),
    ...(link ? { integration: { name: link.integrationName, profile: link.profile } } : {}),
  };
}

export function registerIpc(): void {
  onUpdateStatus(publishUpdate);

  handle("octo:mcp:url", () => mcpUrl());
  // The editor's report about itself, which the menu is drawn from. Rebuilt only on a
  // change: the page reports on every render, and most renders change nothing here.
  handle("octo:menu:status", (_event, status) => {
    if (setEditorStatus(status)) buildMenu();
  });
  handle("octo:update:status", () => updateStatus());
  // The page may ask for the restart, not perform it: the shell still shuts the
  // server down on the way out and decides whether an update is there to install.
  handle("octo:update:install", () => restartToUpdate());
  handle("octo:mcp:copy", () => copyMcpUrl());
  handle("octo:vault:get", () => {
    const vault = openVault();
    return vault ? vaultRef(vault.path) : null;
  });
  handle("octo:vault:recents", () => recents().map((v) => vaultRef(v.path)));
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

  handle("octo:term:open", (event, cols, rows) => {
    const size = (n: unknown, fallback: number) =>
      Number.isInteger(n) && (n as number) > 0 ? (n as number) : fallback;
    return terminal.open(event.sender, size(cols, 80), size(rows, 24));
  });
  listen("octo:term:write", (event, id, data) => terminal.write(event.sender, id, data));
  listen("octo:term:resize", (event, id, cols, rows) =>
    terminal.resize(event.sender, id, cols, rows),
  );
  listen("octo:term:kill", (event, id) => terminal.close(event.sender, id));

  handleSettings("octo:settings:get", () => settingsView());
  handleSettings("octo:settings:setBinary", (_event, name, file) => {
    if (!isRuntimeBinary(name)) throw new Error("unknown binary");
    if (file !== null && typeof file !== "string") throw new Error("bad path");
    return setRuntime(name, file);
  });
  handleSettings("octo:settings:pickBinary", async (_event, name) => {
    if (!isRuntimeBinary(name)) throw new Error("unknown binary");
    const chosen = await pickBinaryFile(`Choose the ${name} binary`);
    // Cancelling is not "use the bundled one", so nothing changes.
    return chosen ? setRuntime(name, chosen) : settingsView();
  });
  handleSettings("octo:settings:setDocker", (_event, file) => {
    if (file !== null && typeof file !== "string") throw new Error("bad path");
    updateSettings({ docker: file ?? undefined });
    return settingsView();
  });
  handleSettings("octo:settings:pickDocker", async () => {
    const chosen = await pickBinaryFile("Choose the docker CLI");
    if (chosen) updateSettings({ docker: chosen });
    return settingsView();
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
  // The Container window's: they run docker, so only that window may ask. Every
  // argument is checked again on the other side; nothing here is passed to docker raw.
  handleContainer("octo:container:overview", () => container.overview());
  handleContainer("octo:container:versions", () => container.versions());
  handleContainer("octo:container:build", (_event, ref, tag) => container.build(ref, tag));
  handleContainer("octo:container:export", (_event, tag) => container.exportContext(tag));
  handleContainer("octo:container:run", (_event, ref) => container.run(ref));
  handleContainer("octo:container:list", () => container.list());
  handleContainer("octo:container:remove", (_event, id) => container.remove(id));
  handleContainer("octo:container:open", (_event, id) => container.open(id));
  handleContainer("octo:container:follow", (_event, id) => container.follow(id));
  handleContainer("octo:container:unfollow", () => container.unfollow());
  handleContainer("octo:container:push", (_event, ref, target) => container.push(ref, target));
  handleContainer("octo:container:cancel", () => container.cancel());
  onContainerClosed(container.windowClosed);

  // The platform's: asked by the editor page and by the Platform window, which are
  // both the editor server's pages. Every argument is checked again in the controller
  // before it reaches octopaas's command line.
  paas.setDialogParent(frontWindow);
  paas.onPush((channel, payload) => {
    mainWindow()?.webContents.send(channel, payload);
    platformWindow()?.webContents.send(channel, payload);
    if (channel === "octo:paas:state") buildMenu();
  });
  handle("octo:paas:open", (_event, focus) => openPlatform(isPlatformFocus(focus) ? focus : "integrations"));
  handle("octo:paas:state", () => paas.state());
  handle("octo:paas:refresh", () => paas.refresh());
  handle("octo:paas:cancel", () => paas.cancel());
  handle("octo:paas:addProfile", (_event, name, url, key) => paas.addProfile(name, url, key));
  handle("octo:paas:setKey", (_event, name, key) => paas.setKey(name, key));
  handle("octo:paas:useProfile", (_event, name) => paas.useProfile(name));
  handle("octo:paas:removeProfile", (_event, name) => paas.removeProfile(name));
  handle("octo:paas:signIn", (_event, name) => paas.signIn(name));
  handle("octo:paas:signOut", (_event, name) => paas.signOut(name));
  handle("octo:paas:pickProjectsDir", () => paas.pickProjectsDir());
  handle("octo:paas:resetProjectsDir", () => paas.resetProjectsDir());
  handle("octo:paas:integrations", (_event, name) => paas.integrations(name));
  handle("octo:paas:cachedIntegrations", (_event, name) => paas.cachedIntegrations(name));
  handle("octo:paas:versions", (_event, name, integrationId) => paas.versions(name, integrationId));
  handle("octo:paas:pull", (_event, name, integrationId, integrationName, tag) =>
    paas.pull(name, integrationId, integrationName, tag),
  );
  handle("octo:paas:openLocal", (_event, dir) => paas.openLocal(dir));
  handle("octo:paas:sync", (_event, dir, direction) => paas.sync(dir, direction));
  handle("octo:paas:push", () => paas.pushVault());
  handle("octo:paas:import", (_event, name, integrationName) => paas.importVault(name, integrationName));
  handle("octo:paas:unlink", () => paas.unlink());
  handle("octo:paas:linkedVersions", () => paas.linkedVersions());
  handle("octo:paas:createVersion", (_event, tag, pushFirst) => paas.createVersion(tag, pushFirst));
  handle("octo:paas:removeVersion", (_event, tag) => paas.removeVersion(tag));
  handle("octo:paas:pullVersion", (_event, tag) => paas.pullVersion(tag));
  handle("octo:paas:deployments", () => paas.deployments());
  handle("octo:paas:deploy", (_event, form, pushFirst) => paas.deploy(form, pushFirst));
  handle("octo:paas:rollout", (_event, deploymentId, form) => paas.rollout(deploymentId, form));
  handle("octo:paas:scale", (_event, deploymentId, replicas) => paas.scale(deploymentId, replicas));
  handle("octo:paas:undeploy", (_event, deploymentId, name) => paas.undeploy(deploymentId, name));
  handle("octo:paas:logs", (_event, deploymentId) => paas.logs(deploymentId));
  handle("octo:paas:follow", (_event, deploymentId) => paas.follow(deploymentId));
  handle("octo:paas:unfollow", () => paas.unfollow());
  onPlatformClosed(paas.windowClosed);

  handleSettings("octo:settings:checkUpdate", () => checkForUpdates(false));
  handleSettings("octo:settings:close", () => closeSettings());
}
