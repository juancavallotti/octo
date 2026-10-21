import { createHash } from "node:crypto";
import { cpSync, readdirSync, readFileSync, rmSync } from "node:fs";
import path from "node:path";

/**
 * Whether a folder and its integration still hold the same thing. octopaas can say
 * when an integration last changed but not what it holds, so each side is compared
 * with itself at the last sync: the folder by its contents, the integration by its
 * lastUpdated. Both are recorded in the folder's link whenever it is pulled or pushed.
 */

/** In step, ahead (changed here), behind (changed on the platform), or both. */
export type SyncState = "synced" | "ahead" | "behind" | "diverged";

/**
 * A digest of what a push would send: every file but the hidden ones, which octopaas
 * leaves behind. Paths are part of it, so adding, removing or renaming a file counts.
 */
export function folderDigest(dir: string): string {
  const hash = createHash("sha256");
  const walk = (rel: string) => {
    const entries = readdirSync(path.join(dir, rel), { withFileTypes: true })
      .filter((e) => !e.name.startsWith("."))
      .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
    for (const e of entries) {
      const child = rel ? `${rel}/${e.name}` : e.name;
      if (e.isDirectory()) walk(child);
      else if (e.isFile()) {
        hash.update(`${child}\0`);
        hash.update(readFileSync(path.join(dir, child)));
        hash.update("\0");
      }
    }
  };
  walk("");
  return hash.digest("hex");
}

/** What the folder and the integration were at their last sync. */
export interface Synced {
  digest: string;
  lastUpdated: string;
}

/**
 * Compare both sides with the last sync. Without one — a folder linked before syncs
 * were recorded — neither side can be vouched for, which is the same as both changed.
 */
export function syncState(last: Synced | undefined, digest: string, lastUpdated: string): SyncState {
  if (!last) return "diverged";
  const ahead = digest !== last.digest;
  const behind = Date.parse(lastUpdated) > Date.parse(last.lastUpdated);
  if (ahead && behind) return "diverged";
  if (ahead) return "ahead";
  if (behind) return "behind";
  return "synced";
}

/**
 * Make a folder hold what a fresh pull left in `pulled`: everything a push would
 * send is replaced, so a file deleted on the platform goes here too, and the hidden
 * entries the folder has of its own — .git, .env, its link — stay. Hidden files the
 * pull carried, such as octopaas's manifest, are written over theirs.
 */
export function replaceContents(pulled: string, dir: string): void {
  for (const e of readdirSync(dir)) {
    if (!e.startsWith(".")) rmSync(path.join(dir, e), { recursive: true, force: true });
  }
  cpSync(pulled, dir, { recursive: true, force: true });
}
