import { createHash } from "node:crypto";
import { watch, type FSWatcher } from "node:fs";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { publish } from "@octo/events";
import { fsRoot, isStoredFile, isTestFile, nameOf } from "./store";

/**
 * Noticing that somebody else changed a flow file — a coding agent editing the YAML
 * directly, a `git checkout`, another program — which is the common case here: the
 * folder is plain files other tools work on. Writes through this app's own endpoints
 * publish for themselves and need no watcher.
 *
 * Started by the SSE route, so it runs only while something is listening, and kept on
 * `globalThis` so per-route module instances and dev HMR share one watcher rather than
 * accumulating them.
 */

/** Writes arrive in bursts — a truncate then a write, or a write then a rename. */
const SETTLE_MS = 150;

interface WatchState {
  watcher?: FSWatcher;
  /** Pending per-file debounce timers. */
  timers: Map<string, NodeJS.Timeout>;
  /**
   * The digest of what we last saw for each file, seeded by every write this app makes
   * (see {@link noteWritten}). It is what keeps a save from being announced back as an
   * outside change.
   */
  digests: Map<string, string>;
}

const store = globalThis as unknown as { __octoFsWatch?: WatchState };

function state(): WatchState {
  store.__octoFsWatch ??= { timers: new Map(), digests: new Map() };
  return store.__octoFsWatch;
}

function digest(content: string): string {
  return createHash("sha1").update(content).digest("hex");
}

/**
 * Record that this app wrote `content` to `id`, so the watcher's notification about
 * it is recognised as our own and dropped.
 */
export function noteWritten(id: string, content: string): void {
  state().digests.set(id, digest(content));
}

/**
 * Whether a filename is one the editor opens. Asked of the store rather than answered
 * here, so the two cannot disagree about which files exist.
 */
function isFlowFile(name: string): boolean {
  return isStoredFile(name);
}

async function announce(id: string): Promise<void> {
  const s = state();
  let content: string;
  try {
    content = await readFile(path.join(fsRoot(), id), "utf8");
  } catch {
    // Deleted or renamed away. There is nothing to reload it to, and the editor
    // holding it is better off keeping what it has than being handed an error.
    s.digests.delete(id);
    return;
  }

  const next = digest(content);
  if (s.digests.get(id) === next) return; // our own write, or a touch that changed nothing
  s.digests.set(id, next);

  // A suite is a different file to the editor than the flow it tests, and it has its
  // own token — sending the flow's would reload the document to say a test changed.
  // The event names the FLOW's id, because that is what an open editor is keyed by.
  publish(
    isTestFile(id)
      ? { type: "integration.tests-updated", id: id.replace(/_test(?=\.ya?ml$)/i, "") }
      : { type: "integration.updated", id, name: nameOf(id) },
  );
}

/** Start watching the flows directory, if it is not already being watched. */
export function startWatching(): void {
  const s = state();
  if (s.watcher) return;
  try {
    // Non-recursive: the store keeps every flow as a plain file in the root (see
    // resolveSafe, which refuses anything nested), so there is nothing below to
    // watch — and recursive watching is the part whose platform support varies.
    s.watcher = watch(fsRoot(), { persistent: false }, (_event, filename) => {
      if (!filename || !isFlowFile(filename)) return;
      const id = filename.toString();
      clearTimeout(s.timers.get(id));
      s.timers.set(
        id,
        setTimeout(() => {
          s.timers.delete(id);
          void announce(id);
        }, SETTLE_MS),
      );
    });
    // A watch that fails later must not take the process with it: the editor simply
    // stops noticing external edits, which is where it was before this existed.
    s.watcher.on("error", () => {
      s.watcher?.close();
      s.watcher = undefined;
    });
  } catch {
    // No watch support, or the folder is gone. Same degradation.
  }
}
