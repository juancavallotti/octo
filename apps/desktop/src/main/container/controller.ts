import { dialog, shell } from "electron";
import path from "node:path";
import { editorStatus, sendCommand } from "../editorStatus";
import { stateDir } from "../paths";
import { read, write } from "../state";
import { openVault } from "../vault";
import { containerDialog, pickExportFolder, toContainer } from "../containerWindow";
import { baseTags } from "./baseImages";
import { writeContext } from "./context";
import * as ops from "./containers";
import { dockerCli, dockerStatus, refreshDocker } from "./status";

/**
 * What the Container window's buttons do, between its bridge and the docker
 * operations: one long operation at a time, its output streamed to the window, and
 * the editor's unsaved edits saved first — through the menu's own "save" command, so
 * the shell learns nothing about the editor it did not already know.
 */

/** How long to wait for the editor to report that a save landed. */
const SAVE_TIMEOUT_MS = 5000;

let running: AbortController | null = null;
let following: AbortController | null = null;

function folder(): string {
  const vault = openVault();
  if (!vault) throw new Error("No folder is open");
  return vault.path;
}

function cli(): string {
  const file = dockerCli();
  if (!file || dockerStatus().state !== "running") throw new Error("Docker is not running");
  return file;
}

function output(line: string): void {
  toContainer("octo:container:output", line);
}

/**
 * One operation at a time: two builds into one tag race, and the output pane shows
 * one stream. Cancelling aborts the docker process, which docker treats as Ctrl-C.
 */
async function exclusive<T>(label: string, fn: (signal: AbortSignal) => Promise<T>): Promise<T> {
  if (running) throw new Error("Another container operation is still running");
  running = new AbortController();
  toContainer("octo:container:busy", label);
  output(`— ${label}`);
  try {
    const result = await fn(running.signal);
    output("— Done");
    return result;
  } catch (err) {
    // Failures are the window's to print, as every other refused call is; a cancel is
    // named as one rather than as the abort docker died of.
    throw running.signal.aborted ? new Error("Cancelled") : err;
  } finally {
    running = null;
    toContainer("octo:container:busy", null);
  }
}

export function cancel(): void {
  running?.abort();
}

/**
 * Save the editor's pending edits, because what is built is what is on disk. Asked of
 * the editor through the command the File menu sends; done when its next report says
 * there is nothing left to save. An editor that cannot save leaves the choice to the
 * user rather than silently packaging the old file.
 */
async function ensureSaved(): Promise<boolean> {
  if (!editorStatus()?.canSave) return true;
  sendCommand("save");
  const deadline = Date.now() + SAVE_TIMEOUT_MS;
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 100));
    if (!editorStatus()?.canSave) return true;
  }
  const parent = containerDialog();
  const options = {
    type: "warning" as const,
    message: "The editor's changes could not be saved.",
    detail: "The container is made from the files on disk, without the unsaved changes.",
    buttons: ["Cancel", "Continue Without Them"],
    defaultId: 0,
    cancelId: 0,
  };
  const { response } = parent
    ? await dialog.showMessageBox(parent, options)
    : await dialog.showMessageBox(options);
  return response === 1;
}

/** Everything the window draws before anything is clicked. */
export async function overview() {
  const status = await refreshDocker();
  const vault = openVault();
  return {
    docker: status,
    folder: vault ? { name: vault.name, path: vault.path } : null,
    ref: vault ? ops.defaultRef(vault.path) : "",
    pushTargets: read(stateDir()).pushTargets ?? [],
  };
}

export function versions() {
  return baseTags(dockerStatus().state === "running" ? dockerCli() : null);
}

function tagOf(tag: unknown): string {
  if (typeof tag !== "string" || !tag) throw new Error("Choose a runtime version");
  return tag;
}

export async function build(ref: unknown, tag: unknown): Promise<boolean> {
  if (!ops.validRef(ref)) throw new Error("The image name is not an image reference");
  const base = tagOf(tag);
  if (!(await ensureSaved())) return false;
  const file = cli();
  const dir = folder();
  await exclusive(`Building ${ref}`, (signal) =>
    ops.build(file, dir, ref, base, { onLine: output, signal }),
  );
  return true;
}

export async function exportContext(tag: unknown): Promise<string | null> {
  const base = tagOf(tag);
  const dir = folder();
  if (!(await ensureSaved())) return null;
  const dest = await pickExportFolder(`${ops.slug(dir)}-container`);
  if (!dest) return null;
  const files = await writeContext(dir, dest, base);
  output(`— Exported ${files.length} files and a Dockerfile to ${dest}`);
  shell.showItemInFolder(path.join(dest, "Dockerfile"));
  return dest;
}

/** Run the image as the folder's container, asking before replacing the last one. */
export async function run(ref: unknown) {
  if (!ops.validRef(ref)) throw new Error("The image name is not an image reference");
  const file = cli();
  const dir = folder();
  return exclusive(`Running ${ref}`, async () => {
    try {
      return await ops.run(file, dir, ref, false);
    } catch (err) {
      if (!(err instanceof ops.ContainerExists)) throw err;
      const parent = containerDialog();
      const options = {
        type: "question" as const,
        message: `Replace the running ${err.name}?`,
        detail: "The folder's previous container is removed and a new one started.",
        buttons: ["Cancel", "Replace"],
        defaultId: 1,
        cancelId: 0,
      };
      const { response } = parent
        ? await dialog.showMessageBox(parent, options)
        : await dialog.showMessageBox(options);
      if (response !== 1) throw new Error("Not replaced");
      return ops.run(file, dir, ref, true);
    }
  });
}

export function list() {
  return ops.list(cli(), folder());
}

export async function remove(id: unknown): Promise<void> {
  if (following) following.abort();
  await ops.remove(cli(), folder(), id);
}

/** Open a container's HTTP port in the browser, by id so the URL is ours to build. */
export async function open(id: unknown): Promise<void> {
  const c = (await list()).find((x) => x.id === id);
  if (!c?.httpPort) throw new Error("That container has no HTTP port");
  await shell.openExternal(`http://127.0.0.1:${c.httpPort}/`);
}

/** Follow one container's logs into the output pane, replacing any being followed. */
export function follow(id: unknown): void {
  following?.abort();
  const controller = new AbortController();
  following = controller;
  output(`— Logs of ${String(id).slice(0, 12)}`);
  void ops
    .logs(cli(), folder(), id, { onLine: output, signal: controller.signal })
    .catch((err: unknown) => output(`— ${(err as Error).message}`))
    .finally(() => {
      if (following === controller) following = null;
    });
}

export function unfollow(): void {
  following?.abort();
  following = null;
}

/** How many push targets to remember. */
const MAX_TARGETS = 5;

export async function push(ref: unknown, target: unknown): Promise<void> {
  if (!ops.validRef(ref)) throw new Error("The image name is not an image reference");
  if (!ops.validRef(target)) throw new Error("The push target is not an image reference");
  const file = cli();
  await exclusive(`Pushing ${target}`, (signal) =>
    ops.push(file, ref, target, { onLine: output, signal }),
  );
  const state = read(stateDir());
  const targets = [target, ...(state.pushTargets ?? []).filter((t) => t !== target)];
  write(stateDir(), { ...state, pushTargets: targets.slice(0, MAX_TARGETS) });
}

/** Stop whatever is in flight when the window goes away; containers keep running. */
export function windowClosed(): void {
  running?.abort();
  unfollow();
}
