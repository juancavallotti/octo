import { app } from "electron";
import { createWindow, mainWindow } from "./window";

/**
 * Octo Desktop — the standalone editor, as an app that opens a folder.
 *
 * The whole design in one line: this process owns the *shell* (window, menus,
 * which folder is open, where the binaries are) and spawns the existing Next
 * standalone server as a child to own everything else. Nothing about the editor
 * is reimplemented here, which is why the app is this small.
 */

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

  void app.whenReady().then(() => {
    createWindow();

    app.on("activate", () => {
      // Dock click with no window: macOS convention is to make one.
      if (!mainWindow()) createWindow();
    });
  });
}
