"use server";

/**
 * Server actions backing the Testing tab (platform): the signed-in session's door to the
 * dolphin suites an integration keeps.
 *
 * Where a suite lives, and how an edit finds the file it belongs in, is in
 * `app/lib/suiteResources.ts` — shared with the MCP route, which authorizes a bearer API
 * key instead of a session. These wrap that with `withRead`/`withWrite` and nothing else.
 *
 * A suite is a different kind of artifact from `.octo/editor-meta.json` next door, even
 * though both hide under `.octo/`: a suite is meant to be read, committed, and run, not
 * silently maintained on the user's behalf.
 */

import {
  deleteSuiteFile,
  listSuiteFiles,
  saveSuiteFile,
  type SuiteFile,
} from "@/app/lib/suiteResources";
import { withRead, withWrite } from "./_auth";
import type { ActionResult } from "./_client";

/** Every suite stored against an integration, keyed by the flow each one declares. */
export async function listTestSuites(
  integrationId: string,
): Promise<ActionResult<SuiteFile[]>> {
  return withRead(() => listSuiteFiles(integrationId));
}

/** Write a flow's suite, into the resource already holding it when there is one. */
export async function saveTestSuite(
  integrationId: string,
  flow: string,
  content: string,
): Promise<ActionResult<void>> {
  if (!flow.trim()) {
    return { ok: false, error: "a suite must name the flow it tests" };
  }
  return withWrite(() => saveSuiteFile(integrationId, flow, content));
}

/** Delete a flow's suite. A flow with no suite is a no-op. */
export async function deleteTestSuite(
  integrationId: string,
  flow: string,
): Promise<ActionResult<void>> {
  if (!flow.trim()) {
    return { ok: false, error: "a suite must name the flow it tests" };
  }
  return withWrite(() => deleteSuiteFile(integrationId, flow));
}
