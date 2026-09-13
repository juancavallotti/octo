import { app, dialog } from "electron";
import { mkdirSync } from "node:fs";
import path from "node:path";
import { publish, retract } from "./endpoint";
import { buildMenu } from "./menu";
import { stateDir } from "./paths";
import { choosePort, pinnedPort } from "./port";
import { current, start, stop } from "./server";
import { read, remember, write, type Vault } from "./state";
import { confineTo, mainWindow, showSplash, splashHint } from "./window";

/**
 * Which folder is open, and how it changes.
 *
 * A switch restarts the server rather than repointing it: the server process holds
 * state that is not keyed by folder — run sessions, the port pool, staged
 * resources, caches, SSE subscribers — and repointing would leave the old folder's
 * runs visible inside the new one. The port is reused across the switch, so the MCP
 * endpoint survives it.
 */

/** Remembered folders that still exist, most recent first. */
export function recents(): Vault[] {
  const seen = read(stateDir()).recents;
  return seen.filter((v) => v.path !== current()?.vault);
}

export function openVault(): { path: string; name: string } | null {
  const vault = current()?.vault;
  return vault ? { path: vault, name: path.basename(vault) } : null;
}

/** The folder to open at launch: the last one used, else a created default. */
export function initialVault(): string {
  const last = read(stateDir()).lastVault;
  if (last) return last;
  const fallback = path.join(app.getPath("documents"), "Octo");
  // Created rather than merely named: an OCTO_FS_DIR that does not exist serves an
  // empty flow list with no way to say why.
  mkdirSync(fallback, { recursive: true });
  return fallback;
}

/** Record a folder as current, so the next launch reopens it. */
export function rememberVault(vaultPath: string): void {
  write(stateDir(), remember(read(stateDir()), vaultPath));
  app.addRecentDocument(vaultPath);
}

/**
 * Serialises switches. Two menu clicks in quick succession must not interleave
 * two restarts — the second would try to start a server while the first was
 * still stopping one, and both would fight for the port.
 */
let queue: Promise<unknown> = Promise.resolve();

function serialize<T>(fn: () => Promise<T>): Promise<T> {
  const next = queue.then(fn, fn);
  queue = next.catch(() => {});
  return next;
}

/**
 * Ask before killing work the user may not realise is running: stopping the server
 * stops every flow it owns. Asks only when there is something to lose, and treats a
 * server that cannot answer as having nothing running — an unreachable status
 * endpoint must not block the switch.
 */
async function confirmIfRunning(detail: string, verb: string): Promise<boolean> {
  const server = current();
  if (!server) return true;

  let running = 0;
  try {
    // Bounded, because the whole switch is serialised behind this call: a server
    // that accepts the connection without answering would queue every later switch
    // behind it.
    const res = await fetch(`${server.url}/api/run/active`, {
      signal: AbortSignal.timeout(2000),
    });
    if (res.ok) running = ((await res.json()) as { running?: number }).running ?? 0;
  } catch {
    return true;
  }
  if (running === 0) return true;

  const win = mainWindow();
  const options = {
    type: "question" as const,
    message: running === 1 ? "One flow is still running." : `${running} flows are still running.`,
    detail,
    buttons: ["Cancel", running === 1 ? `Stop It and ${verb}` : `Stop Them and ${verb}`],
    defaultId: 0,
    cancelId: 0,
  };
  const { response } = win
    ? await dialog.showMessageBox(win, options)
    : await dialog.showMessageBox(options);
  return response === 1;
}

async function restartOn(vaultPath: string): Promise<boolean> {
  const previous = current();
  // Reuse the port so the MCP URL survives the switch; only re-pick if we have
  // none yet, or the old one has since been taken by something else.
  const port = pinnedPort() ?? previous?.port ?? (await choosePort());

  await showSplash();
  await splashHint(vaultPath);
  // Retract before stopping: a stale endpoint file points at a URL that now
  // answers for a different folder.
  retract(previous?.vault);
  await stop();

  try {
    const server = await start(vaultPath, port);
    rememberVault(vaultPath);
    publish(server);
    // The recents submenu and the folder-dependent items are built from state.
    buildMenu();
    const win = mainWindow();
    if (win) {
      confineTo(win, server.url);
      await win.loadURL(server.url);
    }
    return true;
  } catch (err) {
    // Roll back to the previous folder rather than leaving the user on a splash
    // screen.
    await dialog.showMessageBox({
      type: "error",
      message: `Octo could not open ${path.basename(vaultPath)}.`,
      detail: err instanceof Error ? err.message : String(err),
      buttons: ["OK"],
    });
    if (previous) await restartOn(previous.vault);
    return false;
  }
}

/** Switch to a known folder. */
export function switchTo(vaultPath: string): Promise<boolean> {
  if (vaultPath === current()?.vault) return Promise.resolve(true);
  return serialize(async () => {
    const ok = await confirmIfRunning("Opening a different folder stops them.", "Switch");
    return ok ? restartOn(vaultPath) : false;
  });
}

/**
 * Restart the server on the folder that is already open, through the same
 * serialised restart a folder switch uses, rollback included. Falls back to the
 * remembered folder when no server is running, so a settings change can bring the
 * app up rather than merely record a preference.
 */
export function reopenCurrent(): Promise<boolean> {
  return serialize(async () => {
    // Resolved inside the queue, not before it: a switch may already be waiting its
    // turn, and this would restart onto the folder the user just left.
    const vault = current()?.vault ?? read(stateDir()).lastVault ?? null;
    if (!vault) return false;
    const ok = await confirmIfRunning(
      "Changing the runtime restarts the editor server, which stops them.",
      "Restart",
    );
    return ok ? restartOn(vault) : false;
  });
}

/** Ask for a folder, then switch to it. Resolves to the chosen path, or null. */
export async function pickVault(): Promise<string | null> {
  const win = mainWindow();
  const options = {
    title: "Open Octo Folder",
    message: "Choose a folder for your flows",
    properties: ["openDirectory", "createDirectory"] as const,
    buttonLabel: "Open",
  };
  const result = win
    ? await dialog.showOpenDialog(win, { ...options, properties: [...options.properties] })
    : await dialog.showOpenDialog({ ...options, properties: [...options.properties] });

  const chosen = result.canceled ? null : (result.filePaths[0] ?? null);
  if (chosen) await switchTo(chosen);
  return chosen;
}
