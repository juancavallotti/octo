import path from "node:path";

/**
 * Which executable the editor server runs on.
 *
 * Not `process.execPath`, which is the obvious answer and the wrong one. Packaged,
 * that path is `Octo.app/Contents/MacOS/Octo` — the *bundle's* main executable — and
 * spawning it starts a second instance of the application as far as LaunchServices is
 * concerned, which macOS gives its own bouncing dock tile. ELECTRON_RUN_AS_NODE
 * suppresses the window, not the registration: the tile is the app being launched,
 * not anything the child then does.
 *
 * Electron already ships an executable for exactly this — the helper bundle, whose
 * Info.plist carries LSUIElement and so never appears in the dock. It is the same
 * binary with the same Node, so nothing about the child changes but its identity to
 * the window server.
 *
 * macOS only. Windows and Linux have no dock and no bundle identity, and there
 * `process.execPath` is already right.
 */

/**
 * The helper executable inside a packaged macOS bundle, given `resourcesPath`
 * (`Octo.app/Contents/Resources`) and the product name electron-builder named the
 * helpers after.
 */
export function helperExecutable(resourcesPath: string, productName: string): string {
  const helper = `${productName} Helper`;
  // Frameworks is Resources' sibling, both under Contents.
  return path.join(resourcesPath, "..", "Frameworks", `${helper}.app`, "Contents", "MacOS", helper);
}
