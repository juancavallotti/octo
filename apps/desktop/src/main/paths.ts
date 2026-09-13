import { app } from "electron";
import { existsSync } from "node:fs";
import path from "node:path";
import { helperExecutable } from "./helper";

/**
 * Where the app's moving parts live, in both modes it runs in. Packaged, they are
 * under `process.resourcesPath`, staged there by electron-builder's extraResources;
 * unpackaged, they come from the checkout — the server from
 * `task desktop:stage`, the binaries from the repo's own bin/.
 */

/** The repo root, when running unpackaged from the checkout. */
function repoRoot(): string {
  // dist/main/index.js -> apps/desktop -> apps -> <root>
  return path.resolve(app.getAppPath(), "..", "..");
}

/** The staged Next standalone server tree (the directory, not the entry file). */
export function serverDir(): string {
  return app.isPackaged
    ? path.join(process.resourcesPath, "server")
    : path.join(app.getAppPath(), "build", "server");
}

/**
 * The server entry point. The path mirrors the Dockerfile's staging exactly:
 * `output: "standalone"` nests the entry under its workspace path, so the file is
 * at <tree>/apps/standalone/server.js and NOT at the tree root.
 */
export function serverEntry(): string {
  return path.join(serverDir(), "apps", "standalone", "server.js");
}

/** Directory holding the `octo` and `dolphin` binaries for this platform. */
export function binDir(): string {
  return app.isPackaged
    ? path.join(process.resourcesPath, "bin")
    : path.join(repoRoot(), "bin");
}

/**
 * A bundled binary's absolute path, with the platform's executable suffix.
 *
 * The binary that shipped inside the app, which is not necessarily the one that
 * runs: a user can choose another in Settings — see `binary()` in settings.ts.
 */
export function bundledBinary(name: RuntimeBinary): string {
  return path.join(binDir(), process.platform === "win32" ? `${name}.exe` : name);
}

export type RuntimeBinary = "octo" | "dolphin";

/** Where the shell keeps its own state: the folder list, the port, the settings. */
export function stateDir(): string {
  return app.getPath("userData");
}

/**
 * Where runs are staged: under userData, because the bundle is read-only and
 * code-signed, and outside the vault, because a rendered run config inlines
 * resolved env values that must not land in a folder the user commits.
 */
export function runDir(): string {
  return path.join(app.getPath("userData"), "runs");
}

/**
 * The executable to spawn the editor server on — see helper.ts for why this is not
 * simply `process.execPath`. Falls back to `process.execPath` when the helper is
 * missing: a stray dock icon costs less than an app that cannot start.
 */
export function nodeExecutable(): string {
  if (!app.isPackaged || process.platform !== "darwin") return process.execPath;
  const helper = helperExecutable(process.resourcesPath, app.getName());
  return existsSync(helper) ? helper : process.execPath;
}
