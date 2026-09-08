/**
 * The desktop shell's bridge, as the browser sees it.
 *
 * This app is served three ways — `task dev`, the Docker image, and inside the
 * Electron shell — and only the third one has a shell to talk to. So every
 * consumer goes through {@link desktopBridge}, which is null in the first two.
 * That keeps the fork in one place instead of scattering `typeof window` checks
 * through components, and it means the web build's behaviour is defined by the
 * same code path rather than by an accident of what happens to be undefined.
 *
 * The shape mirrors apps/desktop/src/preload/index.ts. The two are separate
 * declarations of one contract, in packages that cannot import each other: the
 * editor must not depend on the shell, and the shell must not depend on the app
 * it happens to be showing.
 */

export interface VaultRef {
  path: string;
  name: string;
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
