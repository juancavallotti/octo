/**
 * The pure half of driving octopaas: the argument vectors the platform window's forms
 * turn into, the checks that keep user input from being read as a flag, and reading
 * the CLI's stderr. Tested without spawning anything.
 */

/**
 * A profile name, a version tag or an env var name as octopaas takes them on the
 * command line. Starting with a letter or digit is the part that matters: anything
 * starting with "-" would be read as a flag.
 */
const WORD = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const ENV_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;

export function isWord(v: unknown): v is string {
  return typeof v === "string" && WORD.test(v);
}

export function word(v: unknown, what: string): string {
  if (!isWord(v)) throw new Error(`${what} must be letters, digits, dots, dashes or underscores`);
  return v;
}

/** An id the platform handed out, which is all the shell passes back for one. */
export function id(v: unknown, what: string): string {
  if (typeof v !== "string" || !/^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(v)) {
    throw new Error(`${what} is not an id`);
  }
  return v;
}

/** An http(s) URL, the only kind a profile may point at. */
export function apiUrl(v: unknown): string {
  if (typeof v !== "string") throw new Error("The address must be a URL");
  let parsed: URL;
  try {
    parsed = new URL(v.trim());
  } catch {
    throw new Error("The address must be a URL, such as https://api.octopaas.dev");
  }
  if (parsed.protocol !== "https:" && parsed.protocol !== "http:") {
    throw new Error("The address must start with https:// or http://");
  }
  return parsed.toString().replace(/\/+$/, "");
}

/** The flags every call carries: which installation, and output a program can read. */
export function common(profile: string | null): string[] {
  return [...(profile ? ["--profile", profile] : []), "--plain", "-o", "json"];
}

/**
 * The sign-in link, from one line of `auth login --no-browser`'s stderr. The CLI
 * prints it on a line of its own, so a line that is exactly a URL is the link.
 */
export function signInLink(line: string): string | null {
  const trimmed = line.trim();
  return /^https?:\/\/\S+$/.test(trimmed) ? trimmed : null;
}

/**
 * What a failed command said, for showing to the user. octopaas ends a failure with
 * what happened and a hint; anything before that is progress that is no longer news.
 */
export function failure(stderr: string, code: number): string {
  if (code === 130) return "Cancelled";
  const lines = stderr
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter(Boolean);
  return lines.slice(-2).join("\n") || `octopaas exited with ${code}`;
}

/** One NAME=value pair from a deploy form, checked and joined. */
export interface Binding {
  name: string;
  value: string;
}

/** What the Deploy and Rollout forms send. Every field is optional but the version. */
export interface DeployForm {
  version?: string;
  env?: Binding[];
  /** Env vars read from cluster secrets: the value is the secret's name. */
  secrets?: Binding[];
  tracing?: boolean;
  replicas?: number;
  /** Publish it outside the cluster, on this subdomain. */
  subdomain?: string;
}

function bindings(flag: string, list: Binding[] | undefined): string[] {
  const out: string[] = [];
  for (const b of list ?? []) {
    if (!b || typeof b.name !== "string" || typeof b.value !== "string") continue;
    if (b.name === "" && b.value === "") continue;
    if (!ENV_NAME.test(b.name)) throw new Error(`${b.name || "An env var"} is not an env var name`);
    if (flag === "--secret") word(b.value, `The secret for ${b.name}`);
    out.push(`${flag}=${b.name}=${b.value}`);
  }
  return out;
}

function replicas(n: unknown): string[] {
  if (n === undefined || n === null) return [];
  if (!Number.isInteger(n) || (n as number) < 0 || (n as number) > 100) {
    throw new Error("Replicas must be a whole number from 0 to 100");
  }
  return [`--replicas=${n}`];
}

/** The settings both a deploy and a rollout take. */
function rolloutFlags(form: DeployForm): string[] {
  return [
    ...(form.version ? [`--version=${word(form.version, "The version")}`] : []),
    ...bindings("--env", form.env),
    ...bindings("--secret", form.secrets),
    ...(typeof form.tracing === "boolean" ? [`--tracing=${form.tracing}`] : []),
  ];
}

export function deployArgs(integrationId: string, form: DeployForm): string[] {
  return [
    "deployments",
    "create",
    id(integrationId, "The integration"),
    ...rolloutFlags(form),
    ...replicas(form.replicas),
    ...(form.subdomain ? ["--expose=external", `--subdomain=${word(form.subdomain, "The subdomain")}`] : []),
  ];
}

export function rolloutArgs(deploymentId: string, form: DeployForm): string[] {
  const flags = rolloutFlags(form);
  if (flags.length === 0) throw new Error("Choose a version, or change a setting, to roll out");
  return ["deployments", "rollout", id(deploymentId, "The deployment"), ...flags];
}

export function scaleArgs(deploymentId: string, count: unknown): string[] {
  const [flag] = replicas(count);
  if (!flag) throw new Error("Replicas must be a whole number from 0 to 100");
  return ["deployments", "scale", id(deploymentId, "The deployment"), String(count)];
}
