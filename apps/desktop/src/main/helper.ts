import path from "node:path";

/**
 * The helper executable inside a packaged macOS bundle, given `resourcesPath`
 * (`Octo.app/Contents/Resources`) and the product name electron-builder named the
 * helpers after. macOS only.
 *
 * Spawning the bundle's own `process.execPath` instead registers a second instance
 * of the application with LaunchServices, which gets its own dock tile;
 * ELECTRON_RUN_AS_NODE suppresses the window, not the registration. The helper's
 * Info.plist carries LSUIElement, so it never appears in the dock.
 */
export function helperExecutable(resourcesPath: string, productName: string): string {
  const helper = `${productName} Helper`;
  // Frameworks is Resources' sibling, both under Contents.
  return path.join(resourcesPath, "..", "Frameworks", `${helper}.app`, "Contents", "MacOS", helper);
}
