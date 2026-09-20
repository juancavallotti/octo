import { app, dialog } from "electron";
import { autoUpdater } from "electron-updater";
import { settings } from "./settings";
import { mainWindow } from "./window";

/**
 * Checking for, fetching and installing a new Octo Desktop: electron-updater against
 * the GitHub release the build publishes to. The zip target beside the dmg is what
 * carries the update; a dmg alone cannot deliver one.
 *
 * The install is ours to trigger. electron-updater's `autoInstallOnAppQuit` is not
 * the install-on-quit its name promises: on macOS MacUpdater extends AppUpdater
 * rather than BaseUpdater and registers no quit handler at all, so nothing ever
 * installs unless quitAndInstall is called; and the handler the other platforms do
 * get hangs off the `quit` event, which this app's shutdown — before-quit,
 * preventDefault, stop the server, app.exit — never reaches. So the update sat
 * downloaded and was quietly thrown away on every restart.
 */

/** Where the app is between "nothing to do" and "there is a new Octo waiting". */
export type UpdateStage = "idle" | "checking" | "downloading" | "ready" | "error";

export interface UpdateStatus {
  stage: UpdateStage;
  /** The version on offer, once a check has found one. */
  version: string | null;
  /** The version running now, for anything that wants to say "x → y". */
  current: string;
  /** Whole percent of the download, while one is in flight. */
  percent: number | null;
}

/** Whether this build can update itself at all. An unpackaged dev run cannot. */
export function canUpdate(): boolean {
  return app.isPackaged;
}

let status: UpdateStatus = {
  stage: "idle",
  version: null,
  current: app.getVersion(),
  percent: null,
};

const listeners = new Set<(status: UpdateStatus) => void>();

export function updateStatus(): UpdateStatus {
  return status;
}

/**
 * Hear about the update state changing. Everything that shows it — the menu item, the
 * editor's restart button — subscribes here rather than being called by name, because
 * this module is imported by all of them and cannot import them back.
 */
export function onUpdateStatus(listener: (status: UpdateStatus) => void): void {
  listeners.add(listener);
}

function set(patch: Partial<UpdateStatus>): void {
  status = { ...status, ...patch };
  for (const listener of listeners) listener(status);
}

let configured = false;

function configure(): void {
  if (configured) return;
  configured = true;
  // Fetched as soon as one is found, the way VS Code does it: the decision the user
  // is asked to make is when to restart, not whether to download.
  autoUpdater.autoDownload = true;
  // Left on despite installing the update ourselves. On macOS this flag does not mean
  // "install on quit" — it means Squirrel stages the update while the app is still
  // running, which is what makes our quitAndInstall immediate rather than a download
  // racing the shutdown it was asked for.
  autoUpdater.autoInstallOnAppQuit = true;

  autoUpdater.on("download-progress", (progress) => {
    set({ stage: "downloading", percent: Math.round(progress.percent) });
  });
  autoUpdater.on("update-downloaded", (info) => {
    set({ stage: "ready", version: info.version, percent: 100 });
  });
  autoUpdater.on("error", () => {
    // The message is reported by whoever asked for the check; the state only has to
    // stop claiming a download is coming.
    set({ stage: "error", percent: null });
  });
}

async function report(message: string, detail?: string): Promise<void> {
  const win = mainWindow();
  const options = { type: "info" as const, message, detail, buttons: ["OK"] };
  await (win ? dialog.showMessageBox(win, options) : dialog.showMessageBox(options));
}

/** The dialog behind an explicit check that finds an update already waiting. */
async function offerRestart(): Promise<void> {
  const win = mainWindow();
  const options = {
    type: "info" as const,
    message: `Octo ${status.version} is ready to install.`,
    detail: "Octo will restart into the new version.",
    buttons: ["Later", "Restart Now"],
    defaultId: 1,
    cancelId: 0,
  };
  const { response } = await (win
    ? dialog.showMessageBox(win, options)
    : dialog.showMessageBox(options));
  if (response === 1) restartToUpdate();
}

/**
 * Look for an update and say what was found. `silent` speaks only through the status —
 * which is what lights the restart button — while a check the user asked for reports
 * "up to date" and the like in a dialog as well.
 */
export async function checkForUpdates(silent: boolean): Promise<void> {
  if (!canUpdate()) {
    if (!silent) await report("Updates are only available in a packaged build.");
    return;
  }
  configure();

  // Already downloaded: there is nothing left to check, only a restart to offer.
  if (status.stage === "ready") {
    if (!silent) await offerRestart();
    return;
  }

  set({ stage: "checking", percent: null });
  try {
    const result = await autoUpdater.checkForUpdates();
    const version = result?.updateInfo.version;
    if (!version || version === app.getVersion()) {
      set({ stage: "idle", version: null });
      if (!silent) await report("Octo is up to date.", `You have ${app.getVersion()}.`);
      return;
    }

    // autoDownload means the fetch is already under way; the events above carry it
    // the rest of the way, and "ready" is what the user is asked to act on. Read
    // back through updateStatus() rather than the narrowed `status` above: a small
    // update can land while this check is still awaiting its own manifest.
    if (updateStatus().stage !== "ready") set({ stage: "downloading", version });
    if (!silent) {
      await report(
        `Octo ${version} is downloading.`,
        `You have ${app.getVersion()}. Octo will offer to restart when the update is ready.`,
      );
    }
  } catch (err) {
    set({ stage: "error", percent: null });
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

/** Whether there is a downloaded update sitting there waiting to be installed. */
export function updateIsReady(): boolean {
  return status.stage === "ready";
}

/**
 * Quit into the update. Goes through app.quit() rather than the installer directly so
 * the editor server is shut down on the way out like any other quit; the install
 * happens at the end of that shutdown, in installIfReady.
 */
export function restartToUpdate(): void {
  if (!updateIsReady()) return;
  app.quit();
}

/** How long to wait for the installer to take the process down before doing it ourselves. */
const INSTALL_GRACE_MS = 10_000;

/**
 * Hand the staged update to the installer, at the end of the shutdown. Answers
 * whether it took over the quit: on success the installer relaunches Octo itself, so
 * the caller must not exit the process out from under it.
 *
 * Nothing here is load-bearing for quitting. If the installer refuses — an unsigned
 * build is the usual reason on macOS, where Squirrel will not swap a bundle it cannot
 * verify — the grace timer quits anyway rather than leaving a windowless app running.
 */
export function installIfReady(): boolean {
  if (!updateIsReady()) return false;
  try {
    autoUpdater.quitAndInstall();
  } catch {
    return false;
  }
  setTimeout(() => app.exit(0), INSTALL_GRACE_MS).unref();
  return true;
}
