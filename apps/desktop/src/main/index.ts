import { app, dialog, shell } from "electron";
import { mkdirSync } from "node:fs";
import path from "node:path";
import { logPath, recent } from "./log";
import { choosePort, pinnedPort } from "./port";
import { current, onServerCrash, start, stop } from "./server";
import { confineTo, createWindow, mainWindow, splashHint } from "./window";

/**
 * Octo Desktop — the standalone editor, as an app that opens a folder.
 *
 * The whole design in one line: this process owns the *shell* (window, menus,
 * which folder is open, where the binaries are) and spawns the existing Next
 * standalone server as a child to own everything else. Nothing about the editor
 * is reimplemented here, which is why the app is this small.
 */

/**
 * The folder the app opens until it can remember one (that is the next commit).
 * Created rather than merely defaulted, because an OCTO_FS_DIR that does not exist
 * gives the editor an empty flow list and no way to say why.
 */
function defaultVault(): string {
  const vault = path.join(app.getPath("documents"), "Octo");
  mkdirSync(vault, { recursive: true });
  return vault;
}

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
  const vault = defaultVault();

  try {
    await splashHint(vault);
    const port = pinnedPort() ?? (await choosePort());
    const server = await start(vault, port);

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

  void app.whenReady().then(boot);

  app.on("activate", () => {
    // Dock click with no window: macOS convention is to make one. The server
    // outlives the window (see window-all-closed), so this reattaches to the
    // running one rather than booting a second — start() would throw.
    if (mainWindow()) return;
    const win = createWindow();
    const server = current();
    if (!server) return void boot();
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
    void stop().finally(() => app.exit(0));
  });
}
