/**
 * The bridge a hosting shell may expose to the page, as the browser sees it.
 *
 * Every consumer goes through {@link desktopBridge}, which is null when nothing is
 * hosting the page, so the fork lives here rather than in `typeof window` checks
 * spread through components. The shape is a declaration of the contract, not an import
 * of it: neither side may depend on the other.
 */

export interface VaultRef {
  /**
   * Absolute path, which only a hosting shell can answer: `/api/vault` serves the name
   * alone, because a path carries the user's account name and directory layout. Empty
   * in a plain browser.
   */
  path: string;
  name: string;
}

/** How the person using the editor has said it should behave; set in the shell's Settings. */
export interface EditorPrefsView {
  autoLearn: boolean;
}

export interface DesktopBridge {
  platform: string;
  mcpUrl(): Promise<string | null>;
  vault(): Promise<VaultRef | null>;
  recents(): Promise<VaultRef[]>;
  pickVault(): Promise<string | null>;
  switchVault(path: string): Promise<boolean>;
  revealVault(): Promise<void>;
  copyMcpUrl(): Promise<void>;
  prefs(): Promise<EditorPrefsView>;
  onPrefsChanged(listener: (prefs: EditorPrefsView) => void): () => void;
}

/** The bridge, or null when this is a browser rather than the desktop shell. */
export function desktopBridge(): DesktopBridge | null {
  if (typeof window === "undefined") return null;
  return (window as { octoDesktop?: DesktopBridge }).octoDesktop ?? null;
}

/** Whether the app is running inside the desktop shell. */
export function isDesktop(): boolean {
  return desktopBridge() !== null;
}
