"use server";

/**
 * Server actions for the editor's RUN feature (standalone). They drive the
 * in-process @octo/run-host directly (no HTTP, no auth — local-only), keyed by
 * this browser's run namespace (a cookie, minted here when absent). The live log
 * stream stays an SSE route (`/api/run/logs`), which reads the same cookie.
 */

import { probeVersion, start, status, stop, sync } from "@octo/run-host";
import type { RunStatusSnapshot } from "@octo/editor";
import type { ActionResult } from "@octo/http";
import { ensureRunNamespace } from "../run/namespace";
import { fsResourceProvider } from "../run/resources";

/** Whether RUN is available, whether this browser's runner is live, and its version. */
export async function runStatus(): Promise<ActionResult<RunStatusSnapshot>> {
  await probeVersion(); // warm the version cache so status() can read it
  const ns = await ensureRunNamespace();
  return { ok: true, data: status(ns) };
}

/** Render the config and (re)start this browser's runner. */
export async function runStart(
  yaml: string,
): Promise<ActionResult<RunStatusSnapshot>> {
  const ns = await ensureRunNamespace();
  if (!status(ns).available) {
    return { ok: false, error: "Runner not available (OCTO_BIN_PATH unset)." };
  }
  if (typeof yaml !== "string" || yaml.trim() === "") {
    return { ok: false, error: "missing `yaml`" };
  }
  try {
    return {
      ok: true,
      data: await start(ns, yaml, undefined, { resources: fsResourceProvider }),
    };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/** Stop this browser's runner and clean up its config file. */
export async function runStop(): Promise<ActionResult<RunStatusSnapshot>> {
  const ns = await ensureRunNamespace();
  return { ok: true, data: await stop(ns) };
}

/** Rewrite this browser's watched config so the runner hot-reloads. */
export async function runSync(yaml: string): Promise<ActionResult<void>> {
  const ns = await ensureRunNamespace();
  if (typeof yaml !== "string" || yaml.trim() === "") {
    return { ok: false, error: "missing `yaml`" };
  }
  try {
    await sync(ns, yaml, { resources: fsResourceProvider });
    return { ok: true, data: undefined };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}
