import { contextBridge, ipcRenderer } from "electron";

/**
 * The only channel between the page and the shell. Everything here is a request to
 * the shell about the shell — which folder is open, which were open before, what the
 * MCP URL is — or the shell's menu asking the page to act, which the page carries out
 * itself, or a terminal the shell runs for the page. Nothing here reads the folder's
 * contents; the page already has the server it was loaded from for that.
 */

/** How the person using the editor has said it should behave. Set in Settings. */
export interface EditorPrefsView {
  /** Run flows in the background to learn what their messages look like. */
  autoLearn: boolean;
}

/** Where the shell got to in fetching a new version of itself. */
export interface UpdateStatusView {
  stage: "idle" | "checking" | "downloading" | "ready" | "error";
  version: string | null;
  current: string;
  percent: number | null;
}

/** A shell the shell started for this page. */
export interface TerminalSessionView {
  id: string;
  /** The shell's name, "zsh" say. */
  title: string;
}

/** Shells in the open folder, with the editor's octo, dolphin and octopaas first on PATH. */
export interface TerminalBridge {
  open(cols: number, rows: number): Promise<TerminalSessionView>;
  write(id: string, data: string): void;
  resize(id: string, cols: number, rows: number): void;
  kill(id: string): void;
  /** Every session's output, tagged with its id. Returns the unsubscribe. */
  onData(listener: (id: string, chunk: string) => void): () => void;
  /** A session ending, however it ended. Returns the unsubscribe. */
  onExit(listener: (id: string, code: number) => void): () => void;
}

/**
 * The platform, through the octopaas CLI the shell runs: installations and signing in
 * to them, integrations to pull into a folder and push back, versions, deployments
 * and logs. Its payloads are the shell's own shapes, restated by the page that reads
 * them; the bridge only carries them.
 */
export interface PaasBridge {
  /** Open the Platform window at a part of it, or bring it forward. */
  open(focus?: string): Promise<void>;
  state(): Promise<unknown>;
  refresh(): Promise<unknown>;
  /** Hear the platform state change, in either window. Returns the unsubscribe. */
  onState(listener: (state: unknown) => void): () => void;
  /** Each line the running operation prints. Returns the unsubscribe. */
  onOutput(listener: (line: { line: string; stream: string }) => void): () => void;
  /** Each line a followed deployment logs. Returns the unsubscribe. */
  onLog(listener: (line: { deploymentId: string; line: string; stream: string }) => void): () => void;
  /** The shell asking the Platform window to show a part of itself. Returns the unsubscribe. */
  onFocus(listener: (focus: string) => void): () => void;
  cancel(): Promise<void>;
  addProfile(name: string, url: string, key?: string): Promise<unknown>;
  setKey(name: string, key: string): Promise<unknown>;
  useProfile(name: string): Promise<unknown>;
  removeProfile(name: string): Promise<unknown>;
  signIn(name: string): Promise<unknown>;
  signOut(name: string): Promise<unknown>;
  pickProjectsDir(): Promise<unknown>;
  resetProjectsDir(): Promise<unknown>;
  integrations(profile: string): Promise<unknown>;
  versions(profile: string, integrationId: string): Promise<unknown>;
  pull(profile: string, integrationId: string, name: string, tag?: string | null): Promise<boolean>;
  openLocal(path: string): Promise<boolean>;
  push(): Promise<unknown>;
  importFolder(profile: string, name: string): Promise<unknown>;
  unlink(): Promise<unknown>;
  linkedVersions(): Promise<unknown>;
  createVersion(tag: string, pushFirst: boolean): Promise<unknown>;
  removeVersion(tag: string): Promise<unknown>;
  pullVersion(tag: string): Promise<boolean>;
  deployments(): Promise<unknown>;
  deploy(form: unknown, pushFirst: boolean): Promise<unknown>;
  rollout(deploymentId: string, form: unknown): Promise<unknown>;
  scale(deploymentId: string, replicas: number): Promise<unknown>;
  undeploy(deploymentId: string, name: string): Promise<unknown>;
  logs(deploymentId: string): Promise<unknown>;
  follow(deploymentId: string): Promise<void>;
  unfollow(): Promise<void>;
}

/** A push from the shell, as a listener the page can drop again. */
function subscribe<T>(channel: string, listener: (payload: T) => void): () => void {
  const handler = (_event: unknown, payload: T) => listener(payload);
  ipcRenderer.on(channel, handler);
  return () => ipcRenderer.off(channel, handler);
}

const paas: PaasBridge = {
  open: (focus) => ipcRenderer.invoke("octo:paas:open", focus),
  state: () => ipcRenderer.invoke("octo:paas:state"),
  refresh: () => ipcRenderer.invoke("octo:paas:refresh"),
  onState: (listener) => subscribe("octo:paas:state", listener),
  onOutput: (listener) => subscribe("octo:paas:output", listener),
  onLog: (listener) => subscribe("octo:paas:log", listener),
  onFocus: (listener) => subscribe("octo:paas:focus", listener),
  cancel: () => ipcRenderer.invoke("octo:paas:cancel"),
  addProfile: (name, url, key) => ipcRenderer.invoke("octo:paas:addProfile", name, url, key),
  setKey: (name, key) => ipcRenderer.invoke("octo:paas:setKey", name, key),
  useProfile: (name) => ipcRenderer.invoke("octo:paas:useProfile", name),
  removeProfile: (name) => ipcRenderer.invoke("octo:paas:removeProfile", name),
  signIn: (name) => ipcRenderer.invoke("octo:paas:signIn", name),
  signOut: (name) => ipcRenderer.invoke("octo:paas:signOut", name),
  pickProjectsDir: () => ipcRenderer.invoke("octo:paas:pickProjectsDir"),
  resetProjectsDir: () => ipcRenderer.invoke("octo:paas:resetProjectsDir"),
  integrations: (profile) => ipcRenderer.invoke("octo:paas:integrations", profile),
  versions: (profile, integrationId) => ipcRenderer.invoke("octo:paas:versions", profile, integrationId),
  pull: (profile, integrationId, name, tag) =>
    ipcRenderer.invoke("octo:paas:pull", profile, integrationId, name, tag ?? null),
  openLocal: (path) => ipcRenderer.invoke("octo:paas:openLocal", path),
  push: () => ipcRenderer.invoke("octo:paas:push"),
  importFolder: (profile, name) => ipcRenderer.invoke("octo:paas:import", profile, name),
  unlink: () => ipcRenderer.invoke("octo:paas:unlink"),
  linkedVersions: () => ipcRenderer.invoke("octo:paas:linkedVersions"),
  createVersion: (tag, pushFirst) => ipcRenderer.invoke("octo:paas:createVersion", tag, pushFirst),
  removeVersion: (tag) => ipcRenderer.invoke("octo:paas:removeVersion", tag),
  pullVersion: (tag) => ipcRenderer.invoke("octo:paas:pullVersion", tag),
  deployments: () => ipcRenderer.invoke("octo:paas:deployments"),
  deploy: (form, pushFirst) => ipcRenderer.invoke("octo:paas:deploy", form, pushFirst),
  rollout: (deploymentId, form) => ipcRenderer.invoke("octo:paas:rollout", deploymentId, form),
  scale: (deploymentId, replicas) => ipcRenderer.invoke("octo:paas:scale", deploymentId, replicas),
  undeploy: (deploymentId, name) => ipcRenderer.invoke("octo:paas:undeploy", deploymentId, name),
  logs: (deploymentId) => ipcRenderer.invoke("octo:paas:logs", deploymentId),
  follow: (deploymentId) => ipcRenderer.invoke("octo:paas:follow", deploymentId),
  unfollow: () => ipcRenderer.invoke("octo:paas:unfollow"),
};

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
  /** Where the shell got to in fetching a new version of itself. */
  updateStatus(): Promise<UpdateStatusView>;
  /** Hear about that changing while this page is open. Returns the unsubscribe. */
  onUpdateStatus(listener: (status: UpdateStatusView) => void): () => void;
  /** Ask the shell to quit into the downloaded update. */
  installUpdate(): Promise<void>;
  /** Hear a menu item asking the editor to carry out a command. Returns the unsubscribe. */
  onMenuCommand(listener: (command: string) => void): () => void;
  /**
   * Tell the shell what the editor can do right now, which its menu then shows. Null
   * when the page stops reporting.
   */
  reportMenuStatus(status: unknown): Promise<void>;
  terminal: TerminalBridge;
  paas: PaasBridge;
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
  updateStatus: () => ipcRenderer.invoke("octo:update:status"),
  onUpdateStatus: (listener) => {
    const handler = (_event: unknown, status: UpdateStatusView) => listener(status);
    ipcRenderer.on("octo:update:changed", handler);
    return () => ipcRenderer.off("octo:update:changed", handler);
  },
  installUpdate: () => ipcRenderer.invoke("octo:update:install"),
  onMenuCommand: (listener) => {
    const handler = (_event: unknown, command: string) => listener(command);
    ipcRenderer.on("octo:menu:command", handler);
    return () => ipcRenderer.off("octo:menu:command", handler);
  },
  reportMenuStatus: (status) => ipcRenderer.invoke("octo:menu:status", status),
  terminal: {
    open: (cols, rows) => ipcRenderer.invoke("octo:term:open", cols, rows),
    write: (id, data) => ipcRenderer.send("octo:term:write", id, data),
    resize: (id, cols, rows) => ipcRenderer.send("octo:term:resize", id, cols, rows),
    kill: (id) => ipcRenderer.send("octo:term:kill", id),
    onData: (listener) => {
      const handler = (_event: unknown, id: string, chunk: string) => listener(id, chunk);
      ipcRenderer.on("octo:term:data", handler);
      return () => ipcRenderer.off("octo:term:data", handler);
    },
    onExit: (listener) => {
      const handler = (_event: unknown, id: string, code: number) => listener(id, code);
      ipcRenderer.on("octo:term:exit", handler);
      return () => ipcRenderer.off("octo:term:exit", handler);
    },
  },
  paas,
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
