import { existsSync, readdirSync } from "node:fs";
import path from "node:path";

/**
 * Where a pulled integration lands by default: a folder named after it in the
 * Projects folder. The user can choose anywhere else for one pull; this is only what
 * the dialog starts on.
 */

/** A name the filesystem can hold as one folder: no separators, nothing reserved. */
export function folderName(name: string): string {
  const cleaned = name
    .replace(/[/\\:*?"<>|\x00-\x1f]/g, "-")
    .replace(/^\.+/, "")
    .trim();
  return cleaned || "integration";
}

/** A folder pulling into is safe: not there yet, or there and empty. */
export function isFree(dir: string): boolean {
  if (!existsSync(dir)) return true;
  try {
    return readdirSync(dir).length === 0;
  } catch {
    return false;
  }
}

/**
 * The first free folder for an integration. Two installations can each have one
 * called "billing", so the second is told apart by its profile before falling back
 * to a number.
 */
export function defaultTarget(
  projectsDir: string,
  name: string,
  profile: string,
  free: (dir: string) => boolean = isFree,
): string {
  const base = folderName(name);
  const candidates = [base, `${base}-${folderName(profile)}`];
  for (const c of candidates) {
    const dir = path.join(projectsDir, c);
    if (free(dir)) return dir;
  }
  for (let n = 2; ; n++) {
    const dir = path.join(projectsDir, `${candidates[1]}-${n}`);
    if (free(dir)) return dir;
  }
}
