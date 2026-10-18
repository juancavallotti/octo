import { binary, probeVersion } from "../settings";
import { exec } from "./docker";

/**
 * Which runtime an image is built on. The published `octo-runtime` image is the
 * standalone build — no NATS, no Kubernetes — which is what a single container on its
 * own needs; the `-paas` images expect the platform around them.
 */

export const RUNTIME_IMAGE = "juancavallotti/octo-runtime";

const HUB_TAGS = `https://hub.docker.com/v2/namespaces/juancavallotti/repositories/octo-runtime/tags`;

/** One version to build on, and where it can come from. */
export interface BaseTag {
  tag: string;
  /** Published on Docker Hub. */
  remote: boolean;
  /** Already pulled, so a build needs no network. */
  local: boolean;
}

export interface BaseTags {
  tags: BaseTag[];
  /**
   * The version matching the editor's own runtime, when it is one we can build on. The
   * editor's palette and validation come from that binary, so any other version may
   * accept or reject what the editor did not expect.
   */
  matching: string | null;
  /** Docker Hub did not answer; only what is already pulled is listed. */
  offline: boolean;
}

type Fetch = (url: string, init?: { signal?: AbortSignal }) => Promise<{
  ok: boolean;
  json(): Promise<unknown>;
}>;

interface HubPage {
  next?: string | null;
  results?: Array<{ name?: unknown }>;
}

/**
 * The tags Docker Hub lists, newest first as it returns them. Bounded in pages: the
 * list only grows, and nobody picks a runtime from five hundred versions ago in a menu.
 */
export async function hubTags(fetchFn: Fetch = fetch, maxPages = 3): Promise<string[]> {
  const out: string[] = [];
  let url: string | null | undefined = `${HUB_TAGS}?page_size=100`;
  for (let page = 0; url && page < maxPages; page++) {
    const res = await fetchFn(url, { signal: AbortSignal.timeout(8000) });
    if (!res.ok) throw new Error("Docker Hub did not list the runtime's tags");
    const body = (await res.json()) as HubPage;
    for (const r of body.results ?? []) if (typeof r.name === "string") out.push(r.name);
    url = body.next;
  }
  return out;
}

/** The runtime tags already in the local image store. */
export async function localTags(cli: string): Promise<string[]> {
  const { code, stdout } = await exec(cli, ["images", RUNTIME_IMAGE, "--format", "{{.Tag}}"]);
  if (code !== 0) return [];
  return stdout
    .split("\n")
    .map((t) => t.trim())
    .filter((t) => t && t !== "<none>");
}

const SEMVER = /^v?(\d+)\.(\d+)\.(\d+)$/;

/** `latest` first, then releases newest first, then anything else by name. */
export function compareTags(a: string, b: string): number {
  if (a === b) return 0;
  if (a === "latest") return -1;
  if (b === "latest") return 1;
  const va = SEMVER.exec(a);
  const vb = SEMVER.exec(b);
  if (va && vb) {
    for (let i = 1; i <= 3; i++) {
      const d = Number(vb[i]) - Number(va[i]);
      if (d !== 0) return d;
    }
    return 0;
  }
  if (va) return -1;
  if (vb) return 1;
  return a.localeCompare(b);
}

export function mergeTags(remote: string[], local: string[]): BaseTag[] {
  const byTag = new Map<string, BaseTag>();
  for (const tag of remote) byTag.set(tag, { tag, remote: true, local: false });
  for (const tag of local) {
    const seen = byTag.get(tag);
    if (seen) seen.local = true;
    else byTag.set(tag, { tag, remote: false, local: true });
  }
  return [...byTag.values()].sort((a, b) => compareTags(a.tag, b.tag));
}

/** The release a `octo --version` line names (`octo 0.11.9 (built …)`), if any. */
export function releaseOf(version: string | null): string | null {
  const m = version ? /\b(\d+\.\d+\.\d+)\b/.exec(version) : null;
  return m ? m[1] : null;
}

/** Everything the version picker shows. */
export async function baseTags(cli: string | null): Promise<BaseTags> {
  const [remote, local, editor] = await Promise.all([
    hubTags().catch(() => null),
    cli ? localTags(cli) : Promise.resolve([]),
    probeVersion(binary("octo"), "octo"),
  ]);
  const tags = mergeTags(remote ?? [], local);
  const release = releaseOf(editor);
  return {
    tags,
    matching: release && tags.some((t) => t.tag === release) ? release : null,
    offline: remote === null,
  };
}
