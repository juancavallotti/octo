import { mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import type { Synced } from "./sync";

/**
 * Which integration a folder is, on which installation: written when it is pulled or
 * imported, read to push it back and to deploy it. Kept in the folder's own .octo/,
 * so it travels with the folder, and push never sends it — octopaas leaves hidden
 * folders behind.
 *
 * It names its profile, so a folder always goes back to the installation it came
 * from, whichever one is selected anywhere else.
 */

export interface Link {
  profile: string;
  /** The profile's address when the folder was linked, to tell it apart if renamed. */
  url: string;
  integrationId: string;
  integrationName: string;
  /** The version it was pulled from, when it was a version rather than the draft. */
  pulledTag?: string;
  linkedAt: number;
  /** The folder and the integration as of the last pull or push. */
  synced?: Synced;
}

export function linkFile(dir: string): string {
  return path.join(dir, ".octo", "platform.json");
}

/** The folder's link, or null when it has none or it is not one this app wrote. */
export function readLink(dir: string): Link | null {
  try {
    const raw = JSON.parse(readFileSync(linkFile(dir), "utf8")) as Partial<Link> | null;
    if (!raw || typeof raw !== "object") return null;
    const str = (v: unknown) => typeof v === "string" && v !== "";
    if (!str(raw.profile) || !str(raw.url) || !str(raw.integrationId) || !str(raw.integrationName)) {
      return null;
    }
    return {
      profile: raw.profile!,
      url: raw.url!,
      integrationId: raw.integrationId!,
      integrationName: raw.integrationName!,
      ...(str(raw.pulledTag) ? { pulledTag: raw.pulledTag } : {}),
      linkedAt: typeof raw.linkedAt === "number" ? raw.linkedAt : 0,
      ...(raw.synced && str(raw.synced.digest) && str(raw.synced.lastUpdated)
        ? { synced: { digest: raw.synced.digest, lastUpdated: raw.synced.lastUpdated } }
        : {}),
    };
  } catch {
    return null;
  }
}

/** Write the link atomically, the way the app's own state is written. */
export function writeLink(dir: string, link: Link): void {
  const file = linkFile(dir);
  mkdirSync(path.dirname(file), { recursive: true });
  const tmp = `${file}.tmp`;
  writeFileSync(tmp, `${JSON.stringify(link, null, 2)}\n`, "utf8");
  renameSync(tmp, file);
}

export function removeLink(dir: string): void {
  rmSync(linkFile(dir), { force: true });
}
