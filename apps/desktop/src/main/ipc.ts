import { ipcMain, type IpcMainInvokeEvent } from "electron";
import path from "node:path";
import { copyMcpUrl, mcpUrl, revealVault } from "./menu";
import { sameOrigin } from "./origin";
import { current } from "./server";
import { openVault, pickVault, recents, switchTo } from "./vault";

/**
 * The main-process half of the preload bridge.
 *
 * Every handler is guarded on the sender's origin. The renderer is showing a page
 * from the local server, and these channels can move the app between folders on
 * the filesystem — so a frame that is not that page has no business calling them.
 * In practice nothing else can be loaded (navigation is pinned in window.ts), and
 * that is exactly why the check is cheap to keep: it costs nothing and it stays
 * true if someone later relaxes the navigation rule. Both guards go through
 * sameOrigin, so neither can drift into the prefix comparison they both started
 * with — which a URL like http://127.0.0.1:8477@evil.example/ satisfies without
 * being served by us at all.
 */

/** Reject calls from any frame that is not the editor page we loaded. */
function fromEditor(event: IpcMainInvokeEvent): boolean {
  const origin = current()?.url;
  if (!origin) return false;
  const url = event.senderFrame?.url ?? "";
  return sameOrigin(url, origin);
}

type Handler = (event: IpcMainInvokeEvent, ...args: unknown[]) => unknown;

function handle(channel: string, fn: Handler): void {
  ipcMain.handle(channel, (event, ...args) => {
    if (!fromEditor(event)) throw new Error(`${channel}: refused, unexpected sender`);
    return fn(event, ...args);
  });
}

export function registerIpc(): void {
  handle("octo:mcp:url", () => mcpUrl());
  handle("octo:mcp:copy", () => copyMcpUrl());
  handle("octo:vault:get", () => openVault());
  handle("octo:vault:recents", () =>
    recents().map((v) => ({ path: v.path, name: path.basename(v.path) })),
  );
  handle("octo:vault:pick", () => pickVault());
  handle("octo:vault:reveal", () => revealVault());
  handle("octo:vault:switch", (_event, target) => {
    // The renderer may only switch to a folder the shell already knows about.
    // An arbitrary path from the page would let the editor point the server at
    // any directory on the machine without the user ever seeing a dialog.
    if (typeof target !== "string") return false;
    const known = recents().some((v) => v.path === target);
    return known ? switchTo(target) : false;
  });
}
