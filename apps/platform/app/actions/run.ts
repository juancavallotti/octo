"use server";

/**
 * Server actions for the editor's RUN feature. Unlike the orchestrator actions,
 * these don't make HTTP calls — they drive the in-process @octo/run-host directly,
 * keyed by this browser's run namespace (a cookie, minted here when absent). The
 * live log stream stays an SSE route (`/api/run/logs`), which reads the same
 * cookie. status/sync require a session; start/stop require the write roles —
 * matching the route handlers they replace.
 */

import { probeVersion, start, status, stop, sync } from "@octo/run-host";
import type { RunStatusSnapshot } from "@octo/editor";
import type { ActionResult } from "@octo/http";
import { ensureRunNamespace } from "@/app/run/namespace";
import { withRead, withWrite } from "./_auth";

const ENV_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;

/**
 * Validate the optional dev-env map: a plain object of valid env names to string
 * values. Returns the sanitized map, or null if the shape is invalid.
 */
function parseDevEnv(value: unknown): Record<string, string> | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return null;
  }
  const out: Record<string, string> = {};
  for (const [name, val] of Object.entries(value as Record<string, unknown>)) {
    if (!ENV_NAME.test(name) || typeof val !== "string") return null;
    out[name] = val;
  }
  return out;
}

/** Whether RUN is available, whether this browser's runner is live, and its version. */
export async function runStatus(): Promise<ActionResult<RunStatusSnapshot>> {
  return withRead(async () => {
    await probeVersion(); // warm the version cache so status() can read it
    const ns = await ensureRunNamespace();
    return { ok: true, data: status(ns) };
  });
}

/** Render the config and (re)start this browser's runner. */
export async function runStart(
  yaml: string,
  devEnv?: unknown,
): Promise<ActionResult<RunStatusSnapshot>> {
  return withWrite(async () => {
    const ns = await ensureRunNamespace();
    if (!status(ns).available) {
      return { ok: false, error: "Runner not available (OCTO_BIN_PATH unset)." };
    }
    if (typeof yaml !== "string" || yaml.trim() === "") {
      return { ok: false, error: "missing `yaml`" };
    }
    let env: Record<string, string> | undefined;
    if (devEnv !== undefined) {
      const parsed = parseDevEnv(devEnv);
      if (!parsed) return { ok: false, error: "invalid `devEnv`" };
      env = parsed;
    }
    try {
      return { ok: true, data: await start(ns, yaml, env) };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}

/** Stop this browser's runner and clean up its config file. */
export async function runStop(): Promise<ActionResult<RunStatusSnapshot>> {
  return withWrite(async () => {
    const ns = await ensureRunNamespace();
    return { ok: true, data: await stop(ns) };
  });
}

/** Rewrite this browser's watched config so the runner hot-reloads. */
export async function runSync(yaml: string): Promise<ActionResult<void>> {
  return withRead(async () => {
    const ns = await ensureRunNamespace();
    if (typeof yaml !== "string" || yaml.trim() === "") {
      return { ok: false, error: "missing `yaml`" };
    }
    try {
      await sync(ns, yaml);
      return { ok: true, data: undefined };
    } catch (err) {
      return { ok: false, error: (err as Error).message };
    }
  });
}
