import { contextBridge, ipcRenderer } from "electron";

/**
 * The Container window's bridge. Its channels run docker, so they are its alone: the
 * sender check in ipc.ts refuses them from the editor page and from Settings.
 */

export type DockerState = "missing" | "stopped" | "running";

export interface ContainerOverview {
  docker: { state: DockerState; path?: string; client?: string; server?: string; error?: string };
  folder: { name: string; path: string } | null;
  /** The image name to suggest, from the folder's name. */
  ref: string;
  pushTargets: string[];
}

export interface BaseTagsView {
  tags: { tag: string; remote: boolean; local: boolean }[];
  matching: string | null;
  offline: boolean;
}

export interface ContainerView {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  httpPort: number | null;
  adminPort: number | null;
}

export interface OctoContainerBridge {
  overview(): Promise<ContainerOverview>;
  versions(): Promise<BaseTagsView>;
  /** Resolves false when the user chose not to build without their unsaved changes. */
  build(ref: string, tag: string): Promise<boolean>;
  /** Resolves to the folder written, or null when cancelled. */
  exportContext(tag: string): Promise<string | null>;
  run(ref: string): Promise<ContainerView>;
  list(): Promise<ContainerView[]>;
  remove(id: string): Promise<void>;
  open(id: string): Promise<void>;
  follow(id: string): Promise<void>;
  unfollow(): Promise<void>;
  push(ref: string, target: string): Promise<void>;
  cancel(): Promise<void>;
  /** Each line an operation prints. */
  onOutput(listener: (line: string) => void): void;
  /** The running operation's label, or null when it ends. */
  onBusy(listener: (label: string | null) => void): void;
  /** Docker started or stopped while the window was open. */
  onDocker(listener: (docker: ContainerOverview["docker"]) => void): void;
  /** A menu item asked for a part of the window. */
  onFocus(listener: (focus: string) => void): void;
}

const on =
  <T>(channel: string) =>
  (listener: (value: T) => void) => {
    // The event object carries the sender, so only the payload crosses the bridge.
    ipcRenderer.on(channel, (_event, value: T) => listener(value));
  };

const bridge: OctoContainerBridge = {
  overview: () => ipcRenderer.invoke("octo:container:overview"),
  versions: () => ipcRenderer.invoke("octo:container:versions"),
  build: (ref, tag) => ipcRenderer.invoke("octo:container:build", ref, tag),
  exportContext: (tag) => ipcRenderer.invoke("octo:container:export", tag),
  run: (ref) => ipcRenderer.invoke("octo:container:run", ref),
  list: () => ipcRenderer.invoke("octo:container:list"),
  remove: (id) => ipcRenderer.invoke("octo:container:remove", id),
  open: (id) => ipcRenderer.invoke("octo:container:open", id),
  follow: (id) => ipcRenderer.invoke("octo:container:follow", id),
  unfollow: () => ipcRenderer.invoke("octo:container:unfollow"),
  push: (ref, target) => ipcRenderer.invoke("octo:container:push", ref, target),
  cancel: () => ipcRenderer.invoke("octo:container:cancel"),
  onOutput: on<string>("octo:container:output"),
  onBusy: on<string | null>("octo:container:busy"),
  onDocker: on<ContainerOverview["docker"]>("octo:container:docker"),
  onFocus: on<string>("octo:container:focus"),
};

contextBridge.exposeInMainWorld("octoContainer", bridge);
