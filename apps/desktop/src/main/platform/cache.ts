import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import path from "node:path";

/**
 * The integrations each installation last listed, kept on disk: the folder picker
 * shows them at once and refreshes in place, and still has them when the platform
 * cannot be reached. Keyed by the installation's name and address, so a name pointed
 * somewhere else does not show the old platform's integrations.
 */

export interface CachedIntegration {
  id: string;
  name: string;
  lastUpdated: string;
  updatedByName?: string;
  /** The icon chosen for it on the platform, by name. */
  icon?: string;
}

interface Entry {
  url: string;
  rows: CachedIntegration[];
}

export function cacheFile(dir: string): string {
  return path.join(dir, "integrations.json");
}

function readAll(dir: string): Record<string, Entry> {
  try {
    const raw = JSON.parse(readFileSync(cacheFile(dir), "utf8")) as unknown;
    return raw && typeof raw === "object" ? (raw as Record<string, Entry>) : {};
  } catch {
    return {};
  }
}

/** What the installation last listed, or null when it never has (or moved since). */
export function cachedRows(dir: string, profile: string, url: string): CachedIntegration[] | null {
  const entry = readAll(dir)[profile];
  return entry && entry.url === url && Array.isArray(entry.rows) ? entry.rows : null;
}

/** Keep what the installation just listed, written atomically like the app's state. */
export function storeRows(dir: string, profile: string, url: string, rows: CachedIntegration[]): void {
  const all = readAll(dir);
  all[profile] = { url, rows };
  const file = cacheFile(dir);
  mkdirSync(dir, { recursive: true });
  const tmp = `${file}.tmp`;
  writeFileSync(tmp, `${JSON.stringify(all, null, 2)}\n`, "utf8");
  renameSync(tmp, file);
}
