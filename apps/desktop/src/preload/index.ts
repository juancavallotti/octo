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
  /**
   * Hear about a preference changing in the Settings window, which is open beside this
   * page rather than instead of it. Returns the unsubscribe.
   */
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
    // The event object is deliberately not passed on: it carries the sender, and the
    // page has no business with it. Only the payload crosses the bridge.
    const handler = (_event: unknown, prefs: EditorPrefsView) => listener(prefs);
    ipcRenderer.on("octo:prefs:changed", handler);
    return () => ipcRenderer.off("octo:prefs:changed", handler);
  },
};

contextBridge.exposeInMainWorld("octoDesktop", bridge);

/**
 * Tell the page it is being shown by the desktop shell, before it renders.
 *
 * The window hides the macOS title bar and draws the page under it, so the app's
 * header has to leave room for the traffic lights and stand in as the window's
 * drag handle. Both are one CSS rule keyed on this class
 * (`.octo-desktop header` in apps/standalone/app/globals.css) — styling belongs to
 * the app, and the fact that a shell is present belongs to the shell.
 *
 * Set from the preload rather than from the main process after load, because the
 * preload runs before the document does: there is no frame where the header is
 * drawn without the padding.
 */
document.addEventListener("DOMContentLoaded", () => {
  document.documentElement.classList.add("octo-desktop");
});
