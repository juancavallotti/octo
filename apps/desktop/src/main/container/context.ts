import { copyFile, lstat, mkdir, readdir, realpath, stat, writeFile } from "node:fs/promises";
import path from "node:path";

/**
 * The folder as a docker build context. The folder is the integration: the runtime
 * image loads every `*.yaml` in its config directory and merges them, as the editor
 * reads the same folder, so the image gets the folder — minus what is only the
 * editor's or only this machine's.
 */

/**
 * The image every container is built on: the public standalone runtime — no NATS, no
 * Kubernetes — which is what a single container on its own needs. The `-paas` images
 * expect the platform around them.
 */
export const RUNTIME_IMAGE = "juancavallotti/octo-runtime";

/** Where the runtime image reads its config from (its CMD's `--config`). */
export const CONFIG_DIR = "/etc/octo/integrations";

/**
 * What stays out of the image, by folder-relative posix path:
 * - `.env.dev` and `.env` hold this machine's credentials. A local run passes the dev
 *   file at `docker run` time; an image that carried it would publish it on push.
 * - `.octo/` is the editor's own metadata, and `*_test.yaml` are dolphin suites the
 *   runtime skips anyway.
 * - `Dockerfile` and `.dockerignore` are written by us, and a stray one of the
 *   user's would be copied in as config.
 */
export function excluded(rel: string): boolean {
  const parts = rel.split("/");
  const base = parts[parts.length - 1] ?? "";
  if (parts.some((p) => p === ".git" || p === "node_modules" || p === ".DS_Store")) return true;
  if (parts[0] === ".octo") return true;
  if (parts.length === 1 && [".env", ".env.dev", "Dockerfile", ".dockerignore"].includes(base)) {
    return true;
  }
  return /_test\.ya?ml$/.test(base);
}

/**
 * Every file the image gets, folder-relative and sorted. A symlink is followed only
 * when it lands inside the folder: one pointing at `~/.ssh` would otherwise be copied
 * into an image that may be pushed anywhere.
 */
export async function contextFiles(folder: string): Promise<string[]> {
  const root = await realpath(folder);
  const out: string[] = [];

  async function walk(dir: string, rel: string): Promise<void> {
    for (const entry of await readdir(dir, { withFileTypes: true })) {
      const childRel = rel ? `${rel}/${entry.name}` : entry.name;
      if (excluded(childRel)) continue;
      const full = path.join(dir, entry.name);
      let info = await lstat(full);
      if (info.isSymbolicLink()) {
        const target = await realpath(full).catch(() => null);
        if (!target || !inside(root, target)) continue;
        info = await stat(target);
      }
      if (info.isDirectory()) await walk(full, childRel);
      else if (info.isFile()) out.push(childRel);
    }
  }

  await walk(root, "");
  return out.sort();
}

function inside(root: string, target: string): boolean {
  const rel = path.relative(root, target);
  return rel !== "" && !rel.startsWith("..") && !path.isAbsolute(rel);
}

/**
 * The image's Dockerfile. COPY only: the runtime image is distroless, with no shell to
 * RUN anything in. The storage dir is set because the runtime's default is relative
 * to a working directory the base image does not choose.
 */
export function dockerfile(tag: string): string {
  return [
    "# Built by Octo Desktop from the folder beside this file.",
    "#",
    "#   docker build -t my-integration .",
    "#   docker run -p 8080:8080 --env-file .env.dev my-integration",
    "#",
    "# Port 8080 is the HTTP source; 39999 serves /healthz and /readyz.",
    `FROM ${RUNTIME_IMAGE}:${tag}`,
    `COPY --chown=nonroot:nonroot . ${CONFIG_DIR}/`,
    "ENV OCTO_STORAGE_DIR=/home/nonroot/octo-store",
    "EXPOSE 8080 39999",
    "",
  ].join("\n");
}

/** Keeps the build files themselves, and anything a user adds later, out of the COPY. */
export const DOCKERIGNORE = ["Dockerfile", ".dockerignore", ".env", ".env.dev", ""].join("\n");

/**
 * Write the build context for `folder` into `dest`, which must not exist or be empty:
 * writing into a folder that has something in it would ship that something too.
 * Returns the integration files copied.
 */
export async function writeContext(folder: string, dest: string, tag: string): Promise<string[]> {
  const existing = await readdir(dest).catch(() => null);
  if (existing && existing.length > 0) throw new Error(`${dest} is not empty`);
  const files = await contextFiles(folder);
  if (!files.some((f) => !f.includes("/") && /\.ya?ml$/.test(f))) {
    throw new Error("The folder has no integration files to package");
  }
  await mkdir(dest, { recursive: true });
  for (const rel of files) {
    const target = path.join(dest, ...rel.split("/"));
    await mkdir(path.dirname(target), { recursive: true });
    await copyFile(path.join(folder, ...rel.split("/")), target);
  }
  await writeFile(path.join(dest, "Dockerfile"), dockerfile(tag), "utf8");
  await writeFile(path.join(dest, ".dockerignore"), DOCKERIGNORE, "utf8");
  return files;
}
