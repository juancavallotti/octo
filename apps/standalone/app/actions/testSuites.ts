"use server";

/**
 * Server actions backing the Testing tab. A dolphin suite is a real `<flow>_test.yaml`
 * on disk beside the flows, under OCTO_FS_DIR — a committable file that `dolphin test`
 * runs from a terminal, not editor scratch.
 *
 * Storage is flat and shared across flows, so the integration id goes unused: the flow
 * name identifies the suite within the directory.
 */

import type { ActionResult } from "@octo/http";
import {
  deleteSuite,
  listSuites,
  writeSuite,
  type SuiteFile,
} from "../api/fs/testSuiteStore";

/** Every suite stored in the flows directory. */
export async function listTestSuites(): Promise<ActionResult<SuiteFile[]>> {
  try {
    return { ok: true, data: await listSuites() };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/** Write one flow's suite, creating the file when it is new. */
export async function saveTestSuite(
  flow: string,
  content: string,
): Promise<ActionResult<void>> {
  if (typeof flow !== "string" || !flow.trim()) {
    return { ok: false, error: "a suite must name the flow it tests" };
  }
  if (typeof content !== "string") {
    return { ok: false, error: "invalid content" };
  }
  try {
    await writeSuite(flow, content);
    return { ok: true, data: undefined };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/** Delete one flow's suite. */
export async function deleteTestSuite(flow: string): Promise<ActionResult<void>> {
  if (typeof flow !== "string" || !flow.trim()) {
    return { ok: false, error: "a suite must name the flow it tests" };
  }
  try {
    await deleteSuite(flow);
    return { ok: true, data: undefined };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}
