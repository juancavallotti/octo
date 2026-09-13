import { contextBridge, ipcRenderer } from "electron";

/**
 * The only channel between the page and the shell. Everything here is a request to
 * the shell about the shell — which folder is open, which were open before, what the
 * MCP URL is. Nothing here reaches the folder's contents; the page already has the
 * server it was loaded from for that.
 */

/** How the person using the editor has said it should behave. Set in Settings. */
export interface EditorPrefsView {
  /** Run flows in the background to learn what their messages look like. */
  autoLearn: boolean;
}

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
  /** The editor preferences, as Settings last left them. */
  prefs(): Promise<EditorPrefsView>;
  /** Hear about a preference changing while this page is open. Returns the unsubscribe. */
  onPrefsChanged(listener: (prefs: EditorPrefsView) => void): () => void;
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
  prefs: () => ipcRenderer.invoke("octo:prefs:get"),
  onPrefsChanged: (listener) => {
    // The event object carries the sender, so only the payload crosses the bridge.
    const handler = (_event: unknown, prefs: EditorPrefsView) => listener(prefs);
    ipcRenderer.on("octo:prefs:changed", handler);
    return () => ipcRenderer.off("octo:prefs:changed", handler);
  },
};

contextBridge.exposeInMainWorld("octoDesktop", bridge);

/**
 * Tell the page it is being shown by the desktop shell, so it can leave room for the
 * traffic lights the hidden title bar puts over it. Set from the preload, which runs
 * before the document, so there is no frame drawn without that room.
 */
document.addEventListener("DOMContentLoaded", () => {
  document.documentElement.classList.add("octo-desktop");
});
