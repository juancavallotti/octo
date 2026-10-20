import { dialog, shell, type BrowserWindow } from "electron";
import { ensureSaved } from "../editorStatus";
import { mainWindow } from "../window";
import { openVault, recents, rememberVault, switchTo } from "../vault";
import { apiUrl, deployArgs, id, rolloutArgs, scaleArgs, signInLink, word, type DeployForm } from "./args";
import { CliError, list, run, stream } from "./cli";
import { readLink, removeLink, writeLink, type Link } from "./link";
import { projectsDir, setProjectsDir } from "./projects";
import { defaultTarget, isFree } from "./target";

/**
 * What the platform features do, between the bridge and octopaas: one operation at a
 * time, its output streamed to every window that shows the platform, and one picture
 * of the installations and the open folder's link that all of them are drawn from.
 */

/** One installation, as octopaas lists it and as it answered who you are. */
export interface ProfileView {
  name: string;
  url: string;
  /** The CLI's current profile, which a terminal without --profile uses. */
  current: boolean;
  /** How it signs in, in octopaas's words: "API key oct_…", "signed in with …". */
  auth: string;
  identity:
    | { state: "checking" }
    | { state: "signed-out" }
    | { state: "signed-in"; credential: "apiKey" | "oauth"; name: string; email: string; roles: string[] }
    | { state: "error"; message: string };
}

/** The last operation, so a window opened after it started can still say how it went. */
export interface Activity {
  label: string;
  state: "running" | "done" | "failed";
  message?: string;
  at: number;
}

export interface PlatformState {
  profiles: ProfileView[];
  /** Null until octopaas has been asked, and when it could not be run at all. */
  cliError: string | null;
  vault: { path: string; name: string } | null;
  link: Link | null;
  projectsDir: string;
  activity: Activity | null;
  /** The link a sign-in is waiting on, for when no browser opened. */
  signingIn: { profile: string; link: string } | null;
}

let profiles: ProfileView[] = [];
let cliError: string | null = null;
let activity: Activity | null = null;
let signingIn: PlatformState["signingIn"] = null;
let running: AbortController | null = null;
let following: AbortController | null = null;

const listeners = new Set<(channel: string, payload: unknown) => void>();

/** Hear every push, to send it to a window. Returns the unsubscribe. */
export function onPush(fn: (channel: string, payload: unknown) => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function push(channel: string, payload: unknown): void {
  for (const fn of listeners) fn(channel, payload);
}

export function state(): PlatformState {
  const vault = openVault();
  return {
    profiles,
    cliError,
    vault,
    link: vault ? readLink(vault.path) : null,
    projectsDir: projectsDir(),
    activity,
    signingIn,
  };
}

/** Tell every window the state changed. Called after anything that could change it. */
export function publish(): void {
  push("octo:paas:state", state());
}

function output(line: string, source: "stdout" | "stderr"): void {
  push("octo:paas:output", { line, stream: source });
}

/** A dialog's parent: whichever of the shell's windows the user is looking at. */
let dialogParent: () => BrowserWindow | undefined = () => mainWindow() ?? undefined;

export function setDialogParent(fn: () => BrowserWindow | undefined): void {
  dialogParent = fn;
}

async function confirm(message: string, detail: string, verb: string): Promise<boolean> {
  const parent = dialogParent();
  const options = { type: "warning" as const, message, detail, buttons: ["Cancel", verb], defaultId: 0, cancelId: 0 };
  const { response } = parent ? await dialog.showMessageBox(parent, options) : await dialog.showMessageBox(options);
  return response === 1;
}

/**
 * One operation at a time: two pushes of one folder race, and the output shows one
 * stream. Its outcome is kept as the last activity, which both windows show.
 */
async function exclusive<T>(label: string, fn: (signal: AbortSignal) => Promise<T>): Promise<T> {
  if (running) throw new Error(`${activity?.label ?? "Another operation"} is still running`);
  running = new AbortController();
  activity = { label, state: "running", at: Date.now() };
  publish();
  output(`— ${label}`, "stderr");
  try {
    const result = await fn(running.signal);
    activity = { label, state: "done", at: Date.now() };
    output("— Done", "stderr");
    return result;
  } catch (err) {
    const message = running.signal.aborted ? "Cancelled" : err instanceof Error ? err.message : String(err);
    activity = { label, state: "failed", message, at: Date.now() };
    output(`— ${message}`, "stderr");
    throw new Error(message);
  } finally {
    running = null;
    signingIn = null;
    publish();
  }
}

export function cancel(): void {
  running?.abort();
}

// Installations ---------------------------------------------------------------

interface ProfileRow {
  name: string;
  url: string;
  current: boolean;
  auth: string;
}

interface StatusReport {
  credential: "apiKey" | "oauth" | "none";
  user?: { name: string; email: string; roles: string[] };
}

function profile(name: unknown): string {
  return word(name, "The installation's name");
}

/** Read the installations once, for an operation that arrives before any window asked. */
async function loaded(): Promise<void> {
  if (profiles.length === 0 && cliError === null) await refresh();
}

/** A profile the CLI has, refused otherwise: a folder may name one since removed. */
function known(name: string): ProfileView {
  const p = profiles.find((x) => x.name === name);
  if (!p) throw new Error(`There is no installation called ${name}. Add it in Platform settings.`);
  return p;
}

async function identityOf(name: string): Promise<ProfileView["identity"]> {
  try {
    const report = await run<StatusReport>(["auth", "status"], { profile: name, signal: AbortSignal.timeout(15000) });
    if (!report || report.credential === "none" || !report.user) return { state: "signed-out" };
    const { name: who, email, roles } = report.user;
    return { state: "signed-in", credential: report.credential, name: who, email, roles: roles ?? [] };
  } catch (err) {
    return { state: "error", message: err instanceof Error ? err.message : String(err) };
  }
}

/** Ask one installation who the user is there, and show the answer when it comes. */
async function check(name: string): Promise<void> {
  profiles = profiles.map((p) => (p.name === name ? { ...p, identity: { state: "checking" } } : p));
  publish();
  const identity = await identityOf(name);
  profiles = profiles.map((p) => (p.name === name ? { ...p, identity } : p));
  publish();
}

/**
 * Read the installations again, and ask each who the user is. The list is shown at
 * once; each identity fills in as its installation answers, since one that is down
 * should not hold up the others.
 */
export async function refresh(): Promise<PlatformState> {
  try {
    const rows = await list<ProfileRow>(["profile", "list"], { profile: null });
    cliError = null;
    const before = new Map(profiles.map((p) => [p.name, p.identity]));
    profiles = rows.map((r) => ({
      name: r.name,
      url: r.url,
      current: r.current,
      auth: r.auth,
      identity: before.get(r.name) ?? { state: "checking" },
    }));
  } catch (err) {
    profiles = [];
    cliError = err instanceof Error ? err.message : String(err);
  }
  publish();
  void Promise.all(profiles.map((p) => check(p.name)));
  return state();
}

export async function addProfile(name: unknown, url: unknown, key: unknown): Promise<PlatformState> {
  const n = profile(name);
  const u = apiUrl(url);
  const withKey = typeof key === "string" && key.trim() !== "";
  await run(["profile", "add", n, `--url=${u}`, ...(withKey ? ["--with-key"] : [])], {
    profile: null,
    ...(withKey ? { input: `${(key as string).trim()}\n` } : {}),
  });
  return refresh();
}

export async function setKey(name: unknown, key: unknown): Promise<PlatformState> {
  const n = profile(name);
  if (typeof key !== "string" || key.trim() === "") throw new Error("Paste the API key");
  await run(["profile", "add", n, "--with-key"], { profile: null, input: `${key.trim()}\n` });
  return refresh();
}

export async function useProfile(name: unknown): Promise<PlatformState> {
  await run(["profile", "use", profile(name)], { profile: null });
  return refresh();
}

export async function removeProfile(name: unknown): Promise<PlatformState> {
  const n = profile(name);
  const ok = await confirm(
    `Remove the installation ${n}?`,
    "Its address and its sign-in are forgotten, here and in the octopaas CLI. Nothing on the platform changes.",
    "Remove",
  );
  if (!ok) return state();
  await run(["profile", "rm", n, "--yes"], { profile: null });
  return refresh();
}

/**
 * Sign in to an installation in the browser. octopaas waits on its own loopback
 * callback; the shell only opens the link it prints, and keeps it on show in case
 * no browser came up.
 */
export async function signIn(name: unknown): Promise<PlatformState> {
  const n = profile(name);
  await loaded();
  known(n);
  await exclusive(`Signing in to ${n}`, (signal) =>
    stream(["auth", "login", "--no-browser"], {
      profile: n,
      signal,
      onLine: (line, source) => {
        output(line, source);
        const link = signInLink(line);
        if (link && !signingIn) {
          signingIn = { profile: n, link };
          publish();
          void shell.openExternal(link);
        }
      },
    }),
  );
  await check(n);
  return state();
}

export async function signOut(name: unknown): Promise<PlatformState> {
  const n = profile(name);
  await run(["auth", "logout"], { profile: n });
  await check(n);
  return state();
}

export async function pickProjectsDir(): Promise<PlatformState> {
  const parent = dialogParent();
  const options = {
    title: "Projects Folder",
    message: "Integrations you pull from the platform land here",
    defaultPath: projectsDir(),
    properties: ["openDirectory" as const, "createDirectory" as const],
    buttonLabel: "Choose",
  };
  const result = parent ? await dialog.showOpenDialog(parent, options) : await dialog.showOpenDialog(options);
  const chosen = result.canceled ? null : (result.filePaths[0] ?? null);
  if (chosen) setProjectsDir(chosen);
  publish();
  return state();
}

export function resetProjectsDir(): PlatformState {
  setProjectsDir(null);
  publish();
  return state();
}

// Integrations ----------------------------------------------------------------

export interface IntegrationView {
  id: string;
  name: string;
  lastUpdated: string;
  updatedByName?: string;
  /** A folder on this machine already linked to it, from the open one or the recent ones. */
  local?: string;
}

/** Folders the app knows that are linked to an integration on `profileName`. */
function localCopies(profileName: string): Map<string, string> {
  const out = new Map<string, string>();
  const vault = openVault();
  const dirs = [...(vault ? [vault.path] : []), ...recents().map((v) => v.path)];
  for (const dir of dirs) {
    const link = readLink(dir);
    if (link?.profile === profileName && !out.has(link.integrationId)) out.set(link.integrationId, dir);
  }
  return out;
}

export async function integrations(name: unknown): Promise<IntegrationView[]> {
  const n = profile(name);
  const rows = await list<IntegrationView>(["integrations", "list"], { profile: n });
  const local = localCopies(n);
  return rows.map((r) => ({
    id: r.id,
    name: r.name,
    lastUpdated: r.lastUpdated,
    ...(r.updatedByName ? { updatedByName: r.updatedByName } : {}),
    ...(local.has(r.id) ? { local: local.get(r.id) } : {}),
  }));
}

export interface VersionView {
  id: string;
  tag: string;
  createdAt: string;
}

export async function versions(name: unknown, integrationId: unknown): Promise<VersionView[]> {
  return list<VersionView>(["versions", "list", id(integrationId, "The integration")], { profile: profile(name) });
}

/** Ask where a pull goes, starting from the Projects folder. Null when cancelled. */
async function pickTarget(name: string, profileName: string): Promise<string | null> {
  const parent = dialogParent();
  const options = {
    title: `Pull ${name}`,
    message: "Choose a new or empty folder for it",
    defaultPath: defaultTarget(projectsDir(), name, profileName),
    buttonLabel: "Pull",
    properties: ["createDirectory" as const],
  };
  const result = parent ? await dialog.showSaveDialog(parent, options) : await dialog.showSaveDialog(options);
  if (result.canceled || !result.filePath) return null;
  if (!isFree(result.filePath)) {
    throw new Error(`${result.filePath} already has files in it. Choose a new or empty folder.`);
  }
  return result.filePath;
}

/**
 * Pull an integration, or one of its versions, into a folder and open it. The folder
 * is linked first, so it is the integration's even if the switch is declined.
 */
export async function pull(name: unknown, integrationId: unknown, integrationName: unknown, tag: unknown): Promise<boolean> {
  const n = profile(name);
  const intId = id(integrationId, "The integration");
  const label = typeof integrationName === "string" && integrationName !== "" ? integrationName : intId;
  const version = tag === null || tag === undefined || tag === "" ? null : word(tag, "The version");
  await loaded();
  known(n);
  const target = await pickTarget(version ? `${label}-${version}` : label, n);
  if (!target) return false;

  await exclusive(`Pulling ${label}${version ? ` ${version}` : ""} from ${n}`, (signal) =>
    run(version ? ["versions", "pull", intId, version, target] : ["integrations", "pull", intId, target], {
      profile: n,
      signal,
    }),
  );
  writeLink(target, {
    profile: n,
    url: known(n).url,
    integrationId: intId,
    integrationName: label,
    ...(version ? { pulledTag: version } : {}),
    linkedAt: Date.now(),
  });
  rememberVault(target);
  return switchTo(target);
}

/** Open a folder already linked to an integration; only one the app already knows. */
export function openLocal(dir: unknown): Promise<boolean> {
  const known = typeof dir === "string" && (dir === openVault()?.path || recents().some((v) => v.path === dir));
  if (!known) return Promise.resolve(false);
  return switchTo(dir as string);
}

// The open folder ---------------------------------------------------------------

async function linked(): Promise<{ dir: string; link: Link }> {
  await loaded();
  const vault = openVault();
  if (!vault) throw new Error("No folder is open");
  const link = readLink(vault.path);
  if (!link) throw new Error(`${vault.name} is not on a platform yet. Import it first.`);
  known(link.profile);
  return { dir: vault.path, link };
}

async function saved(): Promise<void> {
  const ok = await ensureSaved(
    dialogParent(),
    "The platform gets the files on disk, without the unsaved changes.",
  );
  if (!ok) throw new Error("Cancelled");
}

async function pushFolder(dir: string, link: Link, signal: AbortSignal): Promise<void> {
  await saved();
  await run(["integrations", "push", link.integrationId, dir], { profile: link.profile, signal });
}

/** Replace the integration's draft with the open folder, saved first. */
export async function pushVault(): Promise<PlatformState> {
  const { dir, link } = await linked();
  await exclusive(`Pushing ${link.integrationName} to ${link.profile}`, (signal) => pushFolder(dir, link, signal));
  return state();
}

/** Make the open folder a new integration on an installation, and link it. */
export async function importVault(name: unknown, integrationName: unknown): Promise<PlatformState> {
  const n = profile(name);
  await loaded();
  known(n);
  const vault = openVault();
  if (!vault) throw new Error("No folder is open");
  if (readLink(vault.path)) throw new Error(`${vault.name} is already on a platform`);
  const wanted =
    typeof integrationName === "string" && integrationName.trim() !== "" ? integrationName.trim() : vault.name;
  if (/[\r\n]/.test(wanted) || wanted.length > 100) throw new Error("The name must be one line, under 100 characters");

  const created = await exclusive(`Importing ${vault.name} into ${n}`, async (signal) => {
    await saved();
    return run<{ id: string; name: string }>(["integrations", "import", vault.path, `--name=${wanted}`], {
      profile: n,
      signal,
    });
  });
  if (!created) throw new Error("octopaas did not say what it created");
  writeLink(vault.path, {
    profile: n,
    url: known(n).url,
    integrationId: created.id,
    integrationName: created.name,
    linkedAt: Date.now(),
  });
  publish();
  return state();
}

/** Forget which integration the open folder is. Nothing on the platform changes. */
export async function unlink(): Promise<PlatformState> {
  const vault = openVault();
  if (!vault) return state();
  const ok = await confirm(
    `Unlink ${vault.name} from the platform?`,
    "The folder and the integration both stay as they are; they just stop being one another's.",
    "Unlink",
  );
  if (ok) removeLink(vault.path);
  publish();
  return state();
}

/** The open folder's versions. */
export async function linkedVersions(): Promise<VersionView[]> {
  const { link } = await linked();
  return versions(link.profile, link.integrationId);
}

/**
 * Freeze the draft as a version. `pushFirst` sends the open folder up before, so the
 * version is what the user sees rather than whatever was last pushed.
 */
export async function createVersion(tag: unknown, pushFirst: unknown): Promise<PlatformState> {
  const t = word(tag, "The version");
  const { dir, link } = await linked();
  await exclusive(`Creating ${link.integrationName} ${t}`, async (signal) => {
    if (pushFirst === true) await pushFolder(dir, link, signal);
    await run(["versions", "create", link.integrationId, t], { profile: link.profile, signal });
  });
  return state();
}

export async function removeVersion(tag: unknown): Promise<PlatformState> {
  const t = word(tag, "The version");
  const { link } = await linked();
  const ok = await confirm(`Delete ${link.integrationName} ${t}?`, "A deployment running it keeps running.", "Delete");
  if (ok) {
    await exclusive(`Deleting ${t}`, (signal) =>
      run(["versions", "rm", link.integrationId, t, "--yes"], { profile: link.profile, signal }),
    );
  }
  return state();
}

/** Open one of the open folder's versions in a folder of its own. */
export async function pullVersion(tag: unknown): Promise<boolean> {
  const { link } = await linked();
  return pull(link.profile, link.integrationId, link.integrationName, tag);
}

// Deployments -----------------------------------------------------------------

export interface DeploymentView {
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

export async function deployments(): Promise<DeploymentView[]> {
  const { link } = await linked();
  const rows = await list<DeploymentView>(["deployments", "list", "-i", link.integrationId], {
    profile: link.profile,
  });
  return rows.map((d) => ({
    id: d.id,
    name: d.name,
    tag: d.tag,
    status: d.status,
    replicas: d.replicas,
    readyReplicas: d.readyReplicas,
    reason: d.reason,
    externalUrl: d.externalUrl,
    internalUrl: d.internalUrl,
    lastUpdated: d.lastUpdated,
  }));
}

/** Streamed: octopaas waits for a deployment to settle, saying how it is going. */
async function settle(label: string, profileName: string, args: string[], before?: (s: AbortSignal) => Promise<void>) {
  await exclusive(label, async (signal) => {
    await before?.(signal);
    await stream(args, { profile: profileName, signal, onLine: output });
  });
  return state();
}

/**
 * Deploy the open folder's integration. Without a version octopaas deploys the draft
 * as it stands, so `pushFirst` sends the folder up before.
 */
export async function deploy(form: unknown, pushFirst: unknown): Promise<PlatformState> {
  const { dir, link } = await linked();
  const f = (form ?? {}) as DeployForm;
  const args = deployArgs(link.integrationId, f);
  return settle(
    `Deploying ${link.integrationName}${f.version ? ` ${f.version}` : ""} on ${link.profile}`,
    link.profile,
    args,
    pushFirst === true && !f.version ? (signal) => pushFolder(dir, link, signal) : undefined,
  );
}

export async function rollout(deploymentId: unknown, form: unknown): Promise<PlatformState> {
  const { link } = await linked();
  return settle(`Rolling out ${link.integrationName}`, link.profile, rolloutArgs(deploymentId as string, (form ?? {}) as DeployForm));
}

export async function scale(deploymentId: unknown, replicas: unknown): Promise<PlatformState> {
  const { link } = await linked();
  return settle(`Scaling ${link.integrationName} to ${replicas}`, link.profile, scaleArgs(deploymentId as string, replicas));
}

export async function undeploy(deploymentId: unknown, name: unknown): Promise<PlatformState> {
  const { link } = await linked();
  const d = id(deploymentId, "The deployment");
  const label = typeof name === "string" && name !== "" ? name : d;
  const ok = await confirm(`Remove the deployment ${label}?`, "Its pods are stopped. The version stays.", "Remove");
  if (!ok) return state();
  await exclusive(`Removing ${label}`, (signal) =>
    run(["deployments", "rm", d, "--yes"], { profile: link.profile, signal }),
  );
  return state();
}

// Logs ------------------------------------------------------------------------

export interface LogEntry {
  ts?: string;
  level?: string;
  message?: string;
  [key: string]: unknown;
}

/** The deployment's stored log, oldest first. */
export async function logs(deploymentId: unknown): Promise<LogEntry[]> {
  const { link } = await linked();
  return list<LogEntry>(["logs", id(deploymentId, "The deployment"), "--limit=300"], { profile: link.profile });
}

/**
 * Follow a deployment's pods, each line pushed as it arrives, until unfollowed or the
 * window closes. Its own controller, so following does not hold up an operation.
 */
export async function follow(deploymentId: unknown): Promise<void> {
  const { link } = await linked();
  const d = id(deploymentId, "The deployment");
  unfollow();
  const controller = new AbortController();
  following = controller;
  stream(["logs", d, "-f"], {
    profile: link.profile,
    signal: controller.signal,
    onLine: (line, source) => push("octo:paas:log", { deploymentId: d, line, stream: source }),
  })
    .catch((err: unknown) => {
      if (!controller.signal.aborted) {
        const message = err instanceof CliError || err instanceof Error ? err.message : String(err);
        push("octo:paas:log", { deploymentId: d, line: `— ${message}`, stream: "stderr" });
      }
    })
    .finally(() => {
      if (following === controller) following = null;
    });
}

export function unfollow(): void {
  following?.abort();
  following = null;
}

/** Forget what only made sense while a platform window was open to show it. */
export function windowClosed(): void {
  unfollow();
}

/** For the menu: whether the open folder is linked, read from the folder itself. */
export function vaultLinked(): boolean {
  const vault = openVault();
  return vault !== null && readLink(vault.path) !== null;
}
