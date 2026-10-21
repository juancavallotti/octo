import { app } from "electron";
import path from "node:path";
import { settings, updateSettings } from "../settings";

/** The Projects folder: the one Settings names, else Documents/Octo. */
export function projectsDir(): string {
  return settings().platform?.projectsDir ?? path.join(app.getPath("documents"), "Octo");
}

/** Choose the Projects folder; null goes back to the default. */
export function setProjectsDir(dir: string | null): void {
  updateSettings({ platform: dir ? { projectsDir: dir } : undefined });
}
