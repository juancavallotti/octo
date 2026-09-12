import { app } from "electron";
import { existsSync } from "node:fs";
import path from "node:path";
import { helperExecutable } from "./helper";

/**
 * Where the app's three moving parts live, in both of the modes this app runs in.
 *
 * Packaged, everything is under `process.resourcesPath`, staged there by
 * electron-builder's extraResources. In development, the repo is the source of
 * truth: the server is staged into apps/desktop/build/server by `task desktop:stage`,
 * and the binaries are the repo's own bin/, which is what `task runtime:build`
 * fills and what the root Taskfile already points OCTO_BIN_PATH at. So a developer
 * who has run `task dev` once already has everything this app needs.
 *
 * One module for all of it because the dev/packaged fork is the thing worth having
 * in one place: it is invisible in testing (dev always works, packaged always
 * breaks) and every path in the app has to make the same choice the same way.
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

/** A binary's absolute path, with the platform's executable suffix. */
export function binary(name: "octo" | "dolphin"): string {
  return path.join(binDir(), process.platform === "win32" ? `${name}.exe` : name);
}

/**
 * Where runs are staged. Under userData rather than anywhere in the bundle: the
 * bundle is read-only and code-signed, and writing into it would invalidate the
 * signature. Deliberately not inside the vault either — a rendered run config
 * inlines resolved env values, and those must not land in the user's folder where
 * they would be one `git add .` from being committed.
 */
export function runDir(): string {
  return path.join(app.getPath("userData"), "runs");
}

/**
 * The executable to spawn the editor server on — see helper.ts for why this is not
 * simply `process.execPath`.
 *
 * Falls back to `process.execPath` when the helper is not where it should be. A
 * missing helper means a bundle we do not recognise, and the cost of the two answers
 * is wildly asymmetric: the wrong-but-present one costs a stray dock icon, and a path
 * that does not exist costs the app starting at all.
 */
export function nodeExecutable(): string {
  if (!app.isPackaged || process.platform !== "darwin") return process.execPath;
  const helper = helperExecutable(process.resourcesPath, app.getName());
  return existsSync(helper) ? helper : process.execPath;
}
