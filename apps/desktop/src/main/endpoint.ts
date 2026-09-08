import { app } from "electron";
import { mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import type { RunningServer } from "./server";

/**
 * Publishing where the editor is, so an agent can find it.
 *
 * The server's URL is also its MCP endpoint, and the point of a deterministic
 * port is that an agent configured against it stays configured. But "configured"
 * still requires a human to have found the URL once, and the natural place to
 * look is the folder they are working in — so the endpoint is written into the
 * vault's own .octo/ directory, beside the editor metadata that already lives
 * there. A Claude Code session `cd`'d into the folder can then discover the
 * editor without knowing that Electron exists.
 *
 * The second copy, in userData, answers a different question: it is the record
 * this app leaves for its own next launch, so a crashed instance's server can be
 * reaped rather than left holding the port.
 */

export interface Endpoint {
  url: string;
  mcpUrl: string;
  port: number;
  vault: string;
  /** The server child's pid, for reaping a previous instance's orphan. */
  serverPid: number;
  version: string;
  startedAt: string;
}

function userDataFile(): string {
  return path.join(app.getPath("userData"), "endpoint.json");
}

/** The endpoint file inside a vault. `.octo/` is already the editor's own dir. */
function vaultFile(vault: string): string {
  return path.join(vault, ".octo", "mcp-endpoint.json");
}

function writeAtomic(file: string, body: string): void {
  mkdirSync(path.dirname(file), { recursive: true });
  const tmp = `${file}.tmp`;
  writeFileSync(tmp, body, "utf8");
  renameSync(tmp, file);
}

/** Record a running server in both places. Best-effort: never fails a launch. */
export function publish(server: RunningServer): Endpoint {
  const endpoint: Endpoint = {
    url: server.url,
    mcpUrl: `${server.url}/mcp`,
    port: server.port,
    vault: server.vault,
    serverPid: server.pid,
    version: app.getVersion(),
    startedAt: new Date().toISOString(),
  };
  const body = `${JSON.stringify(endpoint, null, 2)}\n`;
  for (const file of [userDataFile(), vaultFile(server.vault)]) {
    try {
      writeAtomic(file, body);
    } catch {
      // A read-only vault, or a userData we cannot write. Neither is worth
      // refusing to open a folder over — the URL is still in the menu.
    }
  }
  return endpoint;
}

/** Remove the advertisement for a vault we are no longer serving. */
export function retract(vault?: string): void {
  const files = [userDataFile(), ...(vault ? [vaultFile(vault)] : [])];
  for (const file of files) {
    try {
      rmSync(file, { force: true });
    } catch {
      // Nothing to do on the way out.
    }
  }
}

/**
 * Kill a server left behind by a previous instance that did not shut down
 * cleanly (a crash, a force quit, a SIGKILL).
 *
 * Without this the next launch finds its deterministic port taken by its own
 * ghost, walks to the next one, and silently changes the MCP URL out from under
 * every agent configured against it — which is precisely the failure the
 * deterministic port exists to prevent.
 *
 * The pid is checked before it is signalled, because a pid recorded hours ago may
 * by now belong to something else entirely.
 */
export function reapOrphan(): void {
  let previous: Endpoint;
  try {
    previous = JSON.parse(readFileSync(userDataFile(), "utf8")) as Endpoint;
  } catch {
    return; // Clean shutdown last time, or nothing has ever run.
  }
  const pid = previous.serverPid;
  if (typeof pid !== "number" || pid <= 1) return;

  try {
    // Signal 0 tests for existence without delivering anything.
    process.kill(pid, 0);
  } catch {
    rmSync(userDataFile(), { force: true });
    return; // Already gone.
  }

  try {
    // The group, as ever: the orphan may itself have spawned `octo` children.
    process.kill(process.platform === "win32" ? pid : -pid, "SIGKILL");
  } catch {
    // Not ours to kill, or already gone. Either way, stop trying.
  }
  rmSync(userDataFile(), { force: true });
}
