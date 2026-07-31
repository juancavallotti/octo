/**
 * Where a dolphin test suite lives on the platform, and how to find it again.
 *
 * A suite is stored as an integration resource under `.octo/tests/`, with kind
 * `template` because that is what the orchestrator's resource model offers for a non-env
 * file. Being *undeclared* is what keeps it out of the way: a deployed runtime pulls only
 * the resources its config declares, so nothing ever asks for one of these, and the
 * Resources view already hides everything under `.octo/` — so the two tabs cannot fight
 * over the same file.
 *
 * This module holds the storage rules with **no authorization of its own**, because it
 * has two callers that authorize differently: the editor's server actions, gated by the
 * signed-in session, and the MCP route, which has already checked a bearer API key by the
 * time it gets here. Two copies of "find the resource whose file declares this flow"
 * would be two chances to disagree about which file an edit lands on.
 */

import { flowOfSuite, isSuiteFileName, suiteFileName } from "@octo/editor/runtime";
import type { Resource } from "@/app/model/orchestrator";
import * as client from "@/app/actions/_client";
import type { ActionResult } from "@/app/actions/_client";

/** Where suites live. Under `.octo/`, so the Resources view already hides them. */
const SUITE_PREFIX = ".octo/tests/";

/** The resource kind for a non-env file; nothing renders it. */
const SUITE_KIND = "template";

/** A stored suite: the flow it tests, and its YAML. */
export interface SuiteFile {
  flow: string;
  content: string;
}

/** The suite resources among an integration's resources, in name order. */
function suitesIn(resources: Resource[]): Resource[] {
  return resources
    .filter(
      (r) => r.kind === SUITE_KIND && r.name.startsWith(SUITE_PREFIX) && isSuiteFileName(r.name),
    )
    .sort((a, b) => a.name.localeCompare(b.name));
}

/** Every suite stored against an integration, keyed by the flow each one declares. */
export async function listSuiteFiles(
  integrationId: string,
): Promise<ActionResult<SuiteFile[]>> {
  const res = await client.listResources(integrationId);
  if (!res.ok) return res;
  return {
    ok: true as const,
    data: suitesIn(res.data).map((r) => ({
      flow: flowOfSuite(r.content, r.name),
      content: r.content,
    })),
  };
}

/**
 * The resource name to create this flow's suite under. Two distinct flow names can slug
 * to one stem (`Order Intake` and `order-intake`), so a taken name is de-duplicated
 * rather than letting one suite silently replace the other's.
 */
function freeSuiteName(existing: Resource[], flow: string): string {
  const taken = new Set(existing.map((r) => r.name));
  const first = suiteFileName(flow);
  let name = SUITE_PREFIX + first;
  let n = 2;
  while (taken.has(name)) {
    name = SUITE_PREFIX + first.replace(/_test\.yaml$/, `-${n}_test.yaml`);
    n++;
  }
  return name;
}

/**
 * Write a flow's suite, into the resource already holding it when there is one.
 * Resolving by declaration rather than by name is what makes an edit land on the suite
 * the user is looking at, even when its name no longer matches the flow's slug.
 */
export async function saveSuiteFile(
  integrationId: string,
  flow: string,
  content: string,
): Promise<ActionResult<void>> {
  const res = await client.listResources(integrationId);
  if (!res.ok) return res;
  const match = suitesIn(res.data).find((r) => flowOfSuite(r.content, r.name) === flow);
  const written = match
    ? await client.updateResource(integrationId, match.id, SUITE_KIND, match.name, content)
    : await client.createResource(
        integrationId,
        SUITE_KIND,
        freeSuiteName(res.data, flow),
        content,
      );
  return written.ok ? { ok: true as const, data: undefined } : written;
}

/**
 * Delete a flow's suite. There is no delete-by-name on the orchestrator, so this
 * resolves the resource first and deletes it by id; a flow with no suite is a no-op.
 */
export async function deleteSuiteFile(
  integrationId: string,
  flow: string,
): Promise<ActionResult<void>> {
  const res = await client.listResources(integrationId);
  if (!res.ok) return res;
  const match = suitesIn(res.data).find((r) => flowOfSuite(r.content, r.name) === flow);
  if (!match) return { ok: true as const, data: undefined };
  return client.deleteResource(integrationId, match.id);
}
