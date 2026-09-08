import { contextBridge, ipcRenderer } from "electron";

/**
 * The only channel between the editor page and the shell.
 *
 * Everything here is a *request to the shell about the shell* — which folder is
 * open, which folders were open before, what the MCP URL is. Nothing here touches
 * the vault's contents: the editor already reads and writes those through the
 * server it is loaded from, and giving the renderer a second, privileged path to
 * the same files would be a way to get two answers to one question.
 *
 * The editor reads this behind a `typeof window.octoDesktop !== "undefined"`
 * guard, so the same app served from Docker or `task dev` simply doesn't show the
 * affordances the shell provides.
 */

export interface OctoDesktopBridge {
  /** Marks this as the desktop shell; the editor gates its UI on this object existing. */
  readonly platform: NodeJS.Platform;
  /** The editor's own URL, which is also its MCP endpoint base. */
  mcpUrl(): Promise<string>;
  /** The open vault: absolute path and folder name. */
  vault(): Promise<{ path: string; name: string } | null>;
  /** Recently opened vaults, most recent first. */
  recents(): Promise<{ path: string; name: string }[]>;
  /** Open the native folder picker and switch to the chosen folder. Resolves to it. */
  pickVault(): Promise<string | null>;
  /** Switch to an already-known folder. */
  switchVault(path: string): Promise<boolean>;
  /** Reveal the open vault in Finder/Explorer. */
  revealVault(): Promise<void>;
  /** Copy the MCP endpoint URL to the clipboard. */
  copyMcpUrl(): Promise<void>;
}

const bridge: OctoDesktopBridge = {
  platform: process.platform,
  mcpUrl: () => ipcRenderer.invoke("octo:mcp:url"),
  vault: () => ipcRenderer.invoke("octo:vault:get"),
  recents: () => ipcRenderer.invoke("octo:vault:recents"),
  pickVault: () => ipcRenderer.invoke("octo:vault:pick"),
  switchVault: (path: string) => ipcRenderer.invoke("octo:vault:switch", path),
  revealVault: () => ipcRenderer.invoke("octo:vault:reveal"),
  copyMcpUrl: () => ipcRenderer.invoke("octo:mcp:copy"),
};

contextBridge.exposeInMainWorld("octoDesktop", bridge);
