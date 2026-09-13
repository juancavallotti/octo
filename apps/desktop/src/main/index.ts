import { app, dialog, shell } from "electron";
import { publish, reapOrphan, retract } from "./endpoint";
import { registerIpc } from "./ipc";
import { logPath, recent } from "./log";
import { buildMenu } from "./menu";
import { choosePort, pinnedPort } from "./port";
import { current, onServerCrash, start, stop } from "./server";
import { openSettings } from "./settingsWindow";
import { checkOnLaunch } from "./update";
import { initialVault, rememberVault } from "./vault";
import { confineTo, createWindow, mainWindow, splashHint } from "./window";

/**
 * Octo Desktop — the editor as an app that opens a folder. This process owns the
 * shell (window, menus, which folder is open, where the binaries are) and spawns the
 * Next standalone server as a child to own everything else.
 */

// Before anything reads a path: Electron keys userData and logs on the app name, and
// the package name would put the user's state under "desktop". Setting it here makes
// an unpackaged run agree with a packaged one's productName.
app.setName("Octo");

/**
 * Report a failed start with the server's own last words, which usually say why.
 * Offers Settings as well as Quit: one cause is a chosen runtime binary that does not
 * run, and correcting that setting restarts the server from the splash screen.
 */
async function reportStartFailure(err: unknown): Promise<void> {
  const detail = err instanceof Error ? err.message : String(err);
  const { response } = await dialog.showMessageBox({
    type: "error",
    message: "Octo could not start its editor server.",
    detail,
    buttons: ["Quit", "Show Logs", "Settings…"],
    defaultId: 0,
  });
  if (response === 2) return openSettings();
  if (response === 1) shell.showItemInFolder(logPath());
  app.quit();
}

async function boot(): Promise<void> {
  createWindow();
  const vault = initialVault();

  try {
    await splashHint(vault);
    const port = pinnedPort() ?? (await choosePort());
    const server = await start(vault, port);
    rememberVault(vault);
    publish(server);
    // Rebuilt after the server is up: the MCP URL and reveal-folder items are only
    // meaningful once there is a server and a folder.
    buildMenu();
    // After the server is up, so an update dialog never lands in front of a splash
    // screen.
    checkOnLaunch();

    const win = mainWindow();
    if (!win) return;
    confineTo(win, server.url);
    // http://127.0.0.1 rather than a custom protocol: Server Actions check Origin
    // against Host, and an app:// origin fails that check and every save with it.
    await win.loadURL(server.url);
  } catch (err) {
    await reportStartFailure(err);
  }
}

// One instance: there is one server on one deterministic port and one folder open
// at a time, and a second would fight for the port or serve a different folder from
// the same URL.
if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on("second-instance", () => {
    const win = mainWindow();
    if (!win) return;
    if (win.isMinimized()) win.restore();
    win.focus();
  });

  onServerCrash(() => {
    void dialog
      .showMessageBox({
        type: "error",
        message: "The editor server stopped unexpectedly.",
        detail: recent(12).join("\n"),
        buttons: ["Quit", "Show Logs"],
        defaultId: 0,
      })
      .then(({ response }) => {
        if (response === 1) shell.showItemInFolder(logPath());
        app.quit();
      });
  });

  void app.whenReady().then(() => {
    // Before choosing a port: a force-quit instance may still be holding it, and
    // walking past our own ghost moves the MCP URL.
    reapOrphan();
    registerIpc();
    // A menu before the server is up, so the window is never menu-less.
    buildMenu();
    return boot();
  });

  app.on("activate", () => {
    // Dock click with no window: macOS convention is to make one. The server
    // outlives the window, so this reattaches to the running one rather than
    // booting a second — start() would throw.
    if (mainWindow()) return;
    // Asked before making a window: boot() makes its own, and createWindow()
    // reassigns the module-level handle without closing what it replaces.
    const server = current();
    if (!server) return void boot();
    const win = createWindow();
    confineTo(win, server.url);
    void win.loadURL(server.url);
  });

  // Quitting is the only thing that stops the server, so it must finish before the
  // process goes away, or the next launch races a port that is still held.
  let shuttingDown = false;
  app.on("before-quit", (event) => {
    if (shuttingDown) return;
    event.preventDefault();
    shuttingDown = true;
    retract(current()?.vault);
    void stop().finally(() => app.exit(0));
  });
}
