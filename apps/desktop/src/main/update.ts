import { app, dialog } from "electron";
import { autoUpdater } from "electron-updater";
import { settings } from "./settings";
import { mainWindow } from "./window";

/**
 * Checking for a new Octo Desktop.
 *
 * electron-updater against the GitHub release, which electron-builder already
 * publishes to — the zip target beside the dmg exists for exactly this, because a
 * dmg alone cannot deliver an update.
 *
 * Bundled by esbuild rather than declared as a runtime dependency, which is what
 * keeps apps/desktop free of a production dependency tree for electron-builder to
 * resolve across pnpm's symlink farm. See esbuild.config.mjs.
 */

/** Whether this build can update itself at all. An unpackaged dev run cannot. */
export function canUpdate(): boolean {
  return app.isPackaged;
}

let configured = false;

function configure(): void {
  if (configured) return;
  configured = true;
  // Downloading is the user's decision, made in the dialog below. A packaged app
  // that quietly replaced itself would be a surprise the first time it happened.
  autoUpdater.autoDownload = false;
  autoUpdater.autoInstallOnAppQuit = true;
}

async function report(message: string, detail?: string): Promise<void> {
  const win = mainWindow();
  const options = { type: "info" as const, message, detail, buttons: ["OK"] };
  await (win ? dialog.showMessageBox(win, options) : dialog.showMessageBox(options));
}

/**
 * Look for an update and say what was found.
 *
 * `silent` is the launch check, which only speaks when there is news. The menu item
 * passes false and therefore reports "up to date" as well: a check that is silent on
 * success is a check nobody believes they ran.
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
    // A failed check must never be fatal — the user has a working app and no
    // network, or GitHub is down. Only say so when they asked.
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
