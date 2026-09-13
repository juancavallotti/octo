import { app, dialog } from "electron";
import { autoUpdater } from "electron-updater";
import { settings } from "./settings";
import { mainWindow } from "./window";

/**
 * Checking for a new Octo Desktop: electron-updater against the GitHub release the
 * build publishes to. The zip target beside the dmg is what carries the update; a dmg
 * alone cannot deliver one.
 */

/** Whether this build can update itself at all. An unpackaged dev run cannot. */
export function canUpdate(): boolean {
  return app.isPackaged;
}

let configured = false;

function configure(): void {
  if (configured) return;
  configured = true;
  // Downloading is the user's decision, made in the dialog below.
  autoUpdater.autoDownload = false;
  autoUpdater.autoInstallOnAppQuit = true;
}

async function report(message: string, detail?: string): Promise<void> {
  const win = mainWindow();
  const options = { type: "info" as const, message, detail, buttons: ["OK"] };
  await (win ? dialog.showMessageBox(win, options) : dialog.showMessageBox(options));
}

/**
 * Look for an update and say what was found. `silent` speaks only when there is
 * news; a check the user asked for reports "up to date" as well.
 */
export async function checkForUpdates(silent: boolean): Promise<void> {
  if (!canUpdate()) {
    if (!silent) await report("Updates are only available in a packaged build.");
    return;
  }
  configure();

  try {
    const result = await autoUpdater.checkForUpdates();
    const version = result?.updateInfo.version;
    if (!version || version === app.getVersion()) {
      if (!silent) await report("Octo is up to date.", `You have ${app.getVersion()}.`);
      return;
    }

    const win = mainWindow();
    const options = {
      type: "info" as const,
      message: `Octo ${version} is available.`,
      detail: `You have ${app.getVersion()}. The update installs the next time you quit.`,
      buttons: ["Not Now", "Download"],
      defaultId: 1,
      cancelId: 0,
    };
    const { response } = await (win
      ? dialog.showMessageBox(win, options)
      : dialog.showMessageBox(options));
    if (response !== 1) return;

    await autoUpdater.downloadUpdate();
    await report(`Octo ${version} is ready.`, "It will be installed when you quit Octo.");
  } catch (err) {
    // A failed check is never fatal, and only worth saying when it was asked for.
    if (!silent) {
      await report("Could not check for updates.", err instanceof Error ? err.message : String(err));
    }
  }
}

/** The check at launch, if the user has not turned it off. */
export function checkOnLaunch(): void {
  if (settings().autoUpdateCheck === false) return;
  void checkForUpdates(true);
}
