import type { EditorCommandStatus } from "@octo/editor";

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

/** Where the shell got to in fetching a new version of itself. */
export interface UpdateStatusView {
  stage: "idle" | "checking" | "downloading" | "ready" | "error";
  /** The version on offer, once a check has found one. */
  version: string | null;
  /** The version running now. */
  current: string;
  /** Whole percent of the download, while one is in flight. */
  percent: number | null;
}

/** Shells the desktop shell runs in the open folder for the page. */
export interface TerminalBridge {
  open(cols: number, rows: number): Promise<{ id: string; title: string }>;
  write(id: string, data: string): void;
  resize(id: string, cols: number, rows: number): void;
  kill(id: string): void;
  /** Every session's output, tagged with its id. Returns the unsubscribe. */
  onData(listener: (id: string, chunk: string) => void): () => void;
  onExit(listener: (id: string, code: number) => void): () => void;
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
  updateStatus(): Promise<UpdateStatusView>;
  onUpdateStatus(listener: (status: UpdateStatusView) => void): () => void;
  installUpdate(): Promise<void>;
  /**
   * Hear the shell's menu asking the editor to do something. The value crosses a
   * process boundary, so it is checked before it is acted on. Returns the unsubscribe.
   */
  onMenuCommand(listener: (command: unknown) => void): () => void;
  /** Tell the shell what the editor can do now, for its menu to show; null to stop. */
  reportMenuStatus(status: EditorCommandStatus | null): Promise<void>;
  terminal: TerminalBridge;
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
