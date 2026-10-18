import { contextBridge, ipcRenderer } from "electron";

/**
 * The Settings window's bridge, separate from the editor page's: these channels
 * change which binary the app executes. The sender check in ipc.ts enforces the
 * separation.
 */

export interface BinaryStatusView {
  name: "octo" | "dolphin";
  path: string;
  bundled: string;
  override?: string;
  missing: boolean;
  version: string | null;
}

/** The docker CLI: what was chosen, what was found, and whether its daemon answered. */
export interface DockerView {
  state: "missing" | "stopped" | "running";
  path?: string;
  override?: string;
  client?: string;
  server?: string;
  error?: string;
}

export interface SettingsView {
  binaries: BinaryStatusView[];
  docker: DockerView;
  autoUpdateCheck: boolean;
  /** Whether the editor may run flows by itself to improve its CEL completion. */
  autoLearn: boolean;
  appVersion: string;
  /** False on a build that cannot update itself (an unpackaged dev run). */
  canUpdate: boolean;
}

export interface OctoSettingsBridge {
  get(): Promise<SettingsView>;
  /** Choose a binary; pass null to go back to the bundled one. Resolves to the new view. */
  setBinary(name: "octo" | "dolphin", path: string | null): Promise<SettingsView>;
  /** Open the file picker for a binary, and apply what was chosen. */
  pickBinary(name: "octo" | "dolphin"): Promise<SettingsView>;
  /** Choose the docker CLI; pass null to go back to searching for one. */
  setDocker(path: string | null): Promise<SettingsView>;
  pickDocker(): Promise<SettingsView>;
  setAutoUpdateCheck(enabled: boolean): Promise<SettingsView>;
  setAutoLearn(enabled: boolean): Promise<SettingsView>;
  checkForUpdates(): Promise<void>;
  close(): Promise<void>;
}

const bridge: OctoSettingsBridge = {
  get: () => ipcRenderer.invoke("octo:settings:get"),
  setBinary: (name, file) => ipcRenderer.invoke("octo:settings:setBinary", name, file),
  pickBinary: (name) => ipcRenderer.invoke("octo:settings:pickBinary", name),
  setDocker: (file) => ipcRenderer.invoke("octo:settings:setDocker", file),
  pickDocker: () => ipcRenderer.invoke("octo:settings:pickDocker"),
  setAutoUpdateCheck: (enabled) => ipcRenderer.invoke("octo:settings:autoUpdate", enabled),
  setAutoLearn: (enabled) => ipcRenderer.invoke("octo:settings:autoLearn", enabled),
  checkForUpdates: () => ipcRenderer.invoke("octo:settings:checkUpdate"),
  close: () => ipcRenderer.invoke("octo:settings:close"),
};

contextBridge.exposeInMainWorld("octoSettings", bridge);
