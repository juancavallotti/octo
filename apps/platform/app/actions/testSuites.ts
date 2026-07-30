"use server";

/**
 * Server actions backing the Testing tab (platform). A dolphin suite is stored as an
 * integration resource under `.octo/tests/`, with kind `template` because that is what
 * the orchestrator's resource model offers for a non-env file.
 *
 * Being *undeclared* is what keeps it out of the way: a deployed runtime pulls only the
 * resources its config declares, so nothing ever asks for one of these, and the
 * Resources view already hides everything under `.octo/` — so the two tabs cannot fight
 * over the same file. It is the same arrangement `.octo/editor-meta.json` uses next
 * door, but the artifact is a different kind of thing: a suite is meant to be read,
 * committed, and run, not silently maintained on the user's behalf.
 *
 * A suite's flow is read from the file's own `flow:` key rather than from its resource
 * name, and that derivation happens here rather than in the client store — it is server
 * work, and it keeps the editor package out of the browser bundle.
 */

import { flowOfSuite, isSuiteFileName, suiteFileName } from "@octo/editor/runtime";
import type { Resource } from "@/app/model/orchestrator";
import { withRead, withWrite } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";

/** Where suites live. Under `.octo/`, so the Resources view already hides them. */
const SUITE_PREFIX = ".octo/tests/";

/** The resource kind for a non-env file; nothing renders it. */
const SUITE_KIND = "template";

/**
 * A stored suite: the flow it tests, and its YAML. Not exported — a `"use server"`
 * module may only export async functions, and the shape matches the editor's
 * TestSuiteFile structurally, which is all the store adapter needs.
 */
interface SuiteFile {
  flow: string;
  content: string;
}

/** The suite resources among an integration's resources, in name order. */
function suitesIn(resources: Resource[]): Resource[] {
  return resources
    .filter((r) => r.name.startsWith(SUITE_PREFIX) && isSuiteFileName(r.name))
    .sort((a, b) => a.name.localeCompare(b.name));
}

/** Every suite stored against an integration, keyed by the flow each one declares. */
export async function listTestSuites(
  integrationId: string,
): Promise<ActionResult<SuiteFile[]>> {
  return withRead(async () => {
    const res = await client.listResources(integrationId);
    if (!res.ok) return res;
    return {
      ok: true as const,
      data: suitesIn(res.data).map((r) => ({
        flow: flowOfSuite(r.content, r.name),
        content: r.content,
      })),
    };
  });
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
export async function saveTestSuite(
  integrationId: string,
  flow: string,
  content: string,
): Promise<ActionResult<void>> {
  if (!flow.trim()) {
    return { ok: false, error: "a suite must name the flow it tests" };
  }
  return withWrite(async () => {
    const res = await client.listResources(integrationId);
    if (!res.ok) return res;
    const match = suitesIn(res.data).find((r) => flowOfSuite(r.content, r.name) === flow);
    const written = match
      ? await client.updateResource(
          integrationId,
          match.id,
          SUITE_KIND,
          match.name,
          content,
        )
      : await client.createResource(
          integrationId,
          SUITE_KIND,
          freeSuiteName(res.data, flow),
          content,
        );
    return written.ok ? { ok: true as const, data: undefined } : written;
  });
}

/**
 * Delete a flow's suite. There is no delete-by-name on the orchestrator, so this
 * resolves the resource first and deletes it by id; a flow with no suite is a no-op.
 */
export async function deleteTestSuite(
  integrationId: string,
  flow: string,
): Promise<ActionResult<void>> {
  if (!flow.trim()) {
    return { ok: false, error: "a suite must name the flow it tests" };
  }
  return withWrite(async () => {
    const res = await client.listResources(integrationId);
    if (!res.ok) return res;
    const match = suitesIn(res.data).find((r) => flowOfSuite(r.content, r.name) === flow);
    if (!match) return { ok: true as const, data: undefined };
    return client.deleteResource(integrationId, match.id);
  });
}
