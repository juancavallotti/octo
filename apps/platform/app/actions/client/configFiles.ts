/**
 * An integration's config files: the root-level YAML files the runtime merges into one
 * application. Nested under the owning integration; a file's path is path-like, so it
 * travels as a query parameter, never a path segment.
 */

import { call, enc } from "./http";
import type { ConfigFile } from "@/app/model/orchestrator";
import type { ActionResult } from "@octo/http";


/** The integration's config files, by path, without their content. */
export function listConfigFiles(
  integrationId: string,
): Promise<ActionResult<Omit<ConfigFile, "content">[]>> {
  return call<Omit<ConfigFile, "content">[]>("GET", `/integrations/${enc(integrationId)}/files`);
}

export function getConfigFile(
  integrationId: string,
  path: string,
): Promise<ActionResult<ConfigFile>> {
  return call<ConfigFile>(
    "GET",
    `/integrations/${enc(integrationId)}/files/content?path=${enc(path)}`,
  );
}

/** Every config file with its content: the listing, then each file. */
export async function readConfigFiles(
  integrationId: string,
): Promise<ActionResult<ConfigFile[]>> {
  const listed = await listConfigFiles(integrationId);
  if (!listed.ok) return listed;
  const files = await Promise.all(listed.data.map((f) => getConfigFile(integrationId, f.path)));
  const failed = files.find((f) => !f.ok);
  if (failed && !failed.ok) return failed;
  return { ok: true, data: files.map((f) => (f as { ok: true; data: ConfigFile }).data) };
}

/**
 * Make `files` the integration's whole set of config files in one write: each is
 * created or overwritten, and one not listed is deleted. The orchestrator refuses a
 * set that would not merge into one config.
 */
export function replaceConfigFiles(
  integrationId: string,
  files: { path: string; content: string }[],
  actorId?: string,
): Promise<ActionResult<Omit<ConfigFile, "content">[]>> {
  return call<Omit<ConfigFile, "content">[]>("PUT", `/integrations/${enc(integrationId)}/files`, {
    files,
    actorId,
  });
}
