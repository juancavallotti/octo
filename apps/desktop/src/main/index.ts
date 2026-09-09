import { app, dialog, shell } from "electron";
import { publish, reapOrphan, retract } from "./endpoint";
import { registerIpc } from "./ipc";
import { logPath, recent } from "./log";
import { buildMenu } from "./menu";
import { choosePort, pinnedPort } from "./port";
import { current, onServerCrash, start, stop } from "./server";
import { initialVault, rememberVault } from "./vault";
import { confineTo, createWindow, mainWindow, splashHint } from "./window";

/**
 * Octo Desktop — the standalone editor, as an app that opens a folder.
 *
 * The whole design in one line: this process owns the *shell* (window, menus,
 * which folder is open, where the binaries are) and spawns the existing Next
 * standalone server as a child to own everything else. Nothing about the editor
 * is reimplemented here, which is why the app is this small.
 */

// Before anything reads a path. Electron keys userData and logs on the app name,
// which it takes from package.json "name" — that is "desktop", the workspace
// package, and it would put the user's state in ~/Library/Application Support/desktop.
// productName is what a packaged build uses; setName makes an unpackaged run agree,
// so dev and packaged read the same state file instead of two different ones.
app.setName("Octo");

/** Report a failed start with the server's own last words, which usually say why. */
async function reportStartFailure(err: unknown): Promise<void> {
  const detail = err instanceof Error ? err.message : String(err);
  const { response } = await dialog.showMessageBox({
    type: "error",
    message: "Octo could not start its editor server.",
    detail,
    buttons: ["Quit", "Show Logs"],
    defaultId: 0,
  });
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
    // Rebuilt after the server is up, because two of its items (the MCP URL, the
    // reveal-folder item) are only meaningful once there is a server and a folder.
    buildMenu();

    const win = mainWindow();
    if (!win) return;
    confineTo(win, server.url);
    // Loading over http://127.0.0.1 rather than a custom protocol is required,
    // not stylistic: the editor saves through Next Server Actions, whose CSRF
    // check compares Origin against Host. An app:// origin fails that check and
    // every save with it.
    await win.loadURL(server.url);
  } catch (err) {
    await reportStartFailure(err);
  }
}

// One instance, because there is one server on one deterministic port and one
// vault open at a time. A second instance would either fight for the port or
// silently serve a different folder from the same URL an agent is configured
// against.
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
    // Before choosing a port: a previous instance that was force-quit may still
    // be holding it, and walking past our own ghost would silently move the MCP
    // URL out from under every agent configured against it.
    reapOrphan();
    registerIpc();
    // A menu before the server is up, so the window is never menu-less; rebuilt
    // once it is (and after every vault change, from vault.ts).
    buildMenu();
    return boot();
  });

  app.on("activate", () => {
    // Dock click with no window: macOS convention is to make one. The server
    // outlives the window (see window-all-closed), so this reattaches to the
    // running one rather than booting a second — start() would throw.
    if (mainWindow()) return;
    // Ask what state we are in BEFORE making a window: boot() makes its own, and
    // createWindow() reassigns the module-level handle without closing the old
    // one — so creating first left two windows up, the orphan stuck on the splash
    // forever because mainWindow() only ever returned the newer.
    const server = current();
    if (!server) return void boot();
    const win = createWindow();
    confineTo(win, server.url);
    void win.loadURL(server.url);
  });

  // Quitting is the only thing that stops the server, so it must actually finish
  // before the process goes away — otherwise the next launch races a port that is
  // still held.
  let shuttingDown = false;
  app.on("before-quit", (event) => {
    if (shuttingDown) return;
    event.preventDefault();
    shuttingDown = true;
    retract(current()?.vault);
    void stop().finally(() => app.exit(0));
  });
}
