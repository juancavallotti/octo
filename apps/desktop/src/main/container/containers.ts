import { randomUUID } from "node:crypto";
import { existsSync } from "node:fs";
import { rm } from "node:fs/promises";
import path from "node:path";
import { stateDir } from "../paths";
import { available } from "../port";
import { exec, execOk, type ExecOptions } from "./docker";
import { writeContext } from "./context";

/**
 * What the shell does with docker on the folder's behalf: build its image, run it,
 * list and stop what it ran, and push the image somewhere. Every argument that came
 * from the user is checked here before it reaches an argv, and containers are only
 * touched when they carry the label we put on them — so nothing in the window can
 * stop or tail a container this app did not start.
 */

/** The label every container we run carries, valued with the folder it was run from. */
export const VAULT_LABEL = "dev.octopaas.octo.vault";
const HTTP_LABEL = "dev.octopaas.octo.http";
const ADMIN_LABEL = "dev.octopaas.octo.admin";

/** Inside the container: the runtime's HTTP default and its admin server. */
const HTTP_PORT = 8080;
const ADMIN_PORT = 39999;

/**
 * An image reference: optional registry host (with port), repository path, optional
 * tag. Narrower than docker's grammar — no digests, lowercase repositories — which is
 * all a build or a push target needs, and it cannot start with `-`, so it is never
 * read as a flag.
 */
const TAG = /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$/;
const COMPONENT = "[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*";
const REF = new RegExp(
  `^(?:[A-Za-z0-9][A-Za-z0-9.-]*(?::[0-9]+)?/)?${COMPONENT}(?:/${COMPONENT})*(?::${TAG.source.slice(1, -1)})?$`,
);

export function validRef(ref: unknown): ref is string {
  return typeof ref === "string" && ref.length <= 255 && REF.test(ref);
}

function requireRef(ref: unknown, what: string): string {
  if (!validRef(ref)) throw new Error(`${what} is not an image reference: ${String(ref)}`);
  return ref;
}

/** The folder's name as an image or container name can hold it. */
export function slug(folder: string): string {
  const s = path
    .basename(folder)
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return s || "integration";
}

export function defaultRef(folder: string): string {
  return `octo/${slug(folder)}:dev`;
}

export function containerName(folder: string): string {
  return `octo-${slug(folder)}`;
}

/** Build the folder into `ref`, on the runtime at `tag`, staging outside the folder. */
export async function build(
  cli: string,
  folder: string,
  ref: string,
  tag: string,
  opts: ExecOptions = {},
): Promise<void> {
  requireRef(ref, "The image name");
  if (!TAG.test(tag)) throw new Error(`The runtime version is not a tag: ${tag}`);
  // Staged under userData rather than built from the folder in place: the folder's
  // own files would otherwise be the context, exclusions and all.
  const dir = path.join(stateDir(), "containers", randomUUID());
  try {
    const files = await writeContext(folder, dir, tag);
    opts.onLine?.(`Packaging ${files.length} files on octo-runtime:${tag}`);
    await execOk(cli, ["build", "-t", ref, dir], opts);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

/** The first free loopback port at or above `from`. */
async function freePort(from: number, taken: Set<number>): Promise<number> {
  for (let port = from; port < from + 200; port++) {
    if (!taken.has(port) && (await available(port))) return port;
  }
  throw new Error(`No free port between ${from} and ${from + 199}`);
}

export interface RunPlan {
  name: string;
  folder: string;
  ref: string;
  httpPort: number;
  adminPort: number;
  /** The dev env file, when the folder has one. */
  envFile?: string;
}

/**
 * `docker run` for a plan. Published on loopback only: this is the user's machine,
 * not a server. HTTP_PORT and HTTP_HOST come after the env file and override it: a
 * dev file that moved the port would leave the mapping pointing at nothing.
 */
export function runArgs(plan: RunPlan): string[] {
  return [
    "run",
    "-d",
    "--name",
    plan.name,
    "--label",
    `${VAULT_LABEL}=${plan.folder}`,
    "--label",
    `${HTTP_LABEL}=${plan.httpPort}`,
    "--label",
    `${ADMIN_LABEL}=${plan.adminPort}`,
    "-p",
    `127.0.0.1:${plan.httpPort}:${HTTP_PORT}`,
    "-p",
    `127.0.0.1:${plan.adminPort}:${ADMIN_PORT}`,
    ...(plan.envFile ? ["--env-file", plan.envFile] : []),
    "-e",
    `HTTP_PORT=${HTTP_PORT}`,
    "-e",
    "HTTP_HOST=0.0.0.0",
    plan.ref,
  ];
}

/** Thrown when a container of the folder's name exists and replacing it was not asked for. */
export class ContainerExists extends Error {
  constructor(readonly name: string) {
    super(`A container named ${name} already exists`);
  }
}

/** Run `ref` as the folder's container, replacing the previous one only when asked. */
export async function run(
  cli: string,
  folder: string,
  ref: string,
  replace: boolean,
): Promise<Container> {
  requireRef(ref, "The image name");
  const name = containerName(folder);
  const [existing = "", owner = ""] = (
    await execOk(cli, [
      "ps",
      "-a",
      "--no-trunc",
      "--filter",
      `name=^/${name}$`,
      "--format",
      `{{.ID}}\t{{.Label "${VAULT_LABEL}"}}`,
    ])
  )
    .trim()
    .split("\t");
  if (existing) {
    // Two folders can share a name, and so a container name; only this folder's own
    // container is ours to replace.
    if (owner !== folder) {
      throw new Error(`A container named ${name} belongs to something else; remove it first`);
    }
    if (!replace) throw new ContainerExists(name);
    await execOk(cli, ["rm", "-f", existing]);
  }

  const httpPort = await freePort(18080, new Set());
  const adminPort = await freePort(19999, new Set([httpPort]));
  const envFile = path.join(folder, ".env.dev");
  const plan: RunPlan = {
    name,
    folder,
    ref,
    httpPort,
    adminPort,
    ...(existsSync(envFile) ? { envFile } : {}),
  };
  const id = (await execOk(cli, runArgs(plan))).trim();
  return { id, name, image: ref, state: "running", status: "Started", httpPort, adminPort };
}

/** One of the folder's containers, as the window lists it. */
export interface Container {
  id: string;
  name: string;
  image: string;
  /** docker's own word: running, exited, created… */
  state: string;
  /** docker's sentence: "Up 3 minutes", "Exited (1) 2 seconds ago". */
  status: string;
  httpPort: number | null;
  adminPort: number | null;
}

const LIST_FORMAT = [
  "{{.ID}}",
  "{{.Names}}",
  "{{.Image}}",
  "{{.State}}",
  "{{.Status}}",
  `{{.Label "${HTTP_LABEL}"}}`,
  `{{.Label "${ADMIN_LABEL}"}}`,
].join("\t");

export function parseList(stdout: string): Container[] {
  const port = (s: string | undefined) => (s && /^\d+$/.test(s) ? Number(s) : null);
  return stdout
    .split("\n")
    .filter((line) => line.trim())
    .map((line) => {
      const [id = "", name = "", image = "", state = "", status = "", http, admin] =
        line.split("\t");
      return { id, name, image, state, status, httpPort: port(http), adminPort: port(admin) };
    });
}

/** The containers run from this folder, stopped ones included. */
export async function list(cli: string, folder: string): Promise<Container[]> {
  const out = await execOk(cli, [
    "ps",
    "-a",
    // Full ids, as `docker run -d` prints them: a short one would never match.
    "--no-trunc",
    "--filter",
    `label=${VAULT_LABEL}=${folder}`,
    "--format",
    LIST_FORMAT,
  ]);
  return parseList(out);
}

/** One of this folder's containers by id, or a refusal. */
async function owned(cli: string, folder: string, id: unknown): Promise<Container> {
  const found = typeof id === "string" ? (await list(cli, folder)).find((c) => c.id === id) : null;
  if (!found) throw new Error("Not a container this folder started");
  return found;
}

export async function remove(cli: string, folder: string, id: unknown): Promise<void> {
  const c = await owned(cli, folder, id);
  await execOk(cli, ["rm", "-f", c.id]);
}

/** Follow a container's output until it exits or `signal` aborts. */
export async function logs(
  cli: string,
  folder: string,
  id: unknown,
  opts: ExecOptions,
): Promise<void> {
  const c = await owned(cli, folder, id);
  await exec(cli, ["logs", "-f", "--tail", "200", c.id], opts).catch((err: unknown) => {
    // An abort is how a caller stops following, not a failure.
    if ((err as Error).name !== "AbortError") throw err;
  });
}

/** Push `ref` as `target`, tagging it first when they differ. */
export async function push(
  cli: string,
  ref: string,
  target: string,
  opts: ExecOptions = {},
): Promise<void> {
  requireRef(ref, "The image name");
  requireRef(target, "The push target");
  if (target !== ref) await execOk(cli, ["tag", ref, target]);
  await execOk(cli, ["push", target], opts);
}
