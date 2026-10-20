/**
 * The platform as the desktop shell reports it, restated here as the page reads it.
 * The shell runs the octopaas CLI; these are the shapes it hands back across the
 * bridge. Declared rather than imported, like the rest of the bridge: neither side
 * may depend on the other.
 */

export type Identity =
  | { state: "checking" }
  | { state: "signed-out" }
  | { state: "signed-in"; credential: "apiKey" | "oauth"; name: string; email: string; roles: string[] }
  | { state: "error"; message: string };

/** One installation of the platform: an octopaas profile. */
export interface Installation {
  name: string;
  url: string;
  /** The CLI's current profile, the one a terminal uses without --profile. */
  current: boolean;
  /** How it signs in, in octopaas's words. */
  auth: string;
  identity: Identity;
}

/** Which integration the open folder is, on which installation. */
export interface FolderLink {
  profile: string;
  url: string;
  integrationId: string;
  integrationName: string;
  pulledTag?: string;
  linkedAt: number;
}

export interface Activity {
  label: string;
  state: "running" | "done" | "failed";
  message?: string;
  at: number;
}

export interface PlatformState {
  profiles: Installation[];
  /** Why octopaas could not be run at all, when it could not. */
  cliError: string | null;
  vault: { path: string; name: string } | null;
  link: FolderLink | null;
  projectsDir: string;
  activity: Activity | null;
  signingIn: { profile: string; link: string } | null;
}

export interface IntegrationSummary {
  id: string;
  name: string;
  lastUpdated: string;
  updatedByName?: string;
  /** A folder on this machine already linked to it. */
  local?: string;
}

export interface VersionSummary {
  id: string;
  tag: string;
  createdAt: string;
}

export interface DeploymentSummary {
  id: string;
  name: string;
  tag: string;
  status: string;
  replicas: number;
  readyReplicas: number;
  reason: string;
  externalUrl: string;
  internalUrl: string;
  lastUpdated: string;
}

export interface LogEntry {
  ts?: string;
  level?: string;
  message?: string;
  [key: string]: unknown;
}

export interface Binding {
  name: string;
  value: string;
}

export interface DeployForm {
  version?: string;
  env?: Binding[];
  secrets?: Binding[];
  tracing?: boolean;
  replicas?: number;
  subdomain?: string;
}

/** A part of the Platform window, which the shell may ask it to show. */
export type PlatformTab = "integrations" | "folder" | "deployments" | "logs" | "settings";

/** What the shell's focus names map to; "versions" lives on the folder's tab. */
export function tabFor(focus: unknown): PlatformTab {
  switch (focus) {
    case "folder":
    case "versions":
      return "folder";
    case "deployments":
    case "logs":
    case "settings":
      return focus;
    default:
      return "integrations";
  }
}

/** The platform bridge, present when the desktop shell is hosting the page. */
export interface PaasBridge {
  open(focus?: string): Promise<void>;
  state(): Promise<PlatformState>;
  refresh(): Promise<PlatformState>;
  onState(listener: (state: PlatformState) => void): () => void;
  onOutput(listener: (line: { line: string; stream: string }) => void): () => void;
  onLog(listener: (line: { deploymentId: string; line: string; stream: string }) => void): () => void;
  onFocus(listener: (focus: string) => void): () => void;
  cancel(): Promise<void>;
  addProfile(name: string, url: string, key?: string): Promise<PlatformState>;
  setKey(name: string, key: string): Promise<PlatformState>;
  useProfile(name: string): Promise<PlatformState>;
  removeProfile(name: string): Promise<PlatformState>;
  signIn(name: string): Promise<PlatformState>;
  signOut(name: string): Promise<PlatformState>;
  pickProjectsDir(): Promise<PlatformState>;
  resetProjectsDir(): Promise<PlatformState>;
  integrations(profile: string): Promise<IntegrationSummary[]>;
  versions(profile: string, integrationId: string): Promise<VersionSummary[]>;
  pull(profile: string, integrationId: string, name: string, tag?: string | null): Promise<boolean>;
  openLocal(path: string): Promise<boolean>;
  push(): Promise<PlatformState>;
  importFolder(profile: string, name: string): Promise<PlatformState>;
  unlink(): Promise<PlatformState>;
  linkedVersions(): Promise<VersionSummary[]>;
  createVersion(tag: string, pushFirst: boolean): Promise<PlatformState>;
  removeVersion(tag: string): Promise<PlatformState>;
  pullVersion(tag: string): Promise<boolean>;
  deployments(): Promise<DeploymentSummary[]>;
  deploy(form: DeployForm, pushFirst: boolean): Promise<PlatformState>;
  rollout(deploymentId: string, form: DeployForm): Promise<PlatformState>;
  scale(deploymentId: string, replicas: number): Promise<PlatformState>;
  undeploy(deploymentId: string, name: string): Promise<PlatformState>;
  logs(deploymentId: string): Promise<LogEntry[]>;
  follow(deploymentId: string): Promise<void>;
  unfollow(): Promise<void>;
}
