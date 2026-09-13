import { app } from "electron";
import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { serverEntry } from "./paths";
import type { RunningServer } from "./server";

/**
 * Where the running editor is, published to two files that answer two questions.
 * The copy in the vault's `.octo/` advertises the MCP URL to anything working in
 * that folder; the copy in userData is the record this app leaves for its own next
 * launch, so a crashed instance's server can be reaped rather than left holding the
 * port.
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
 * Is this PID still the editor server we started, rather than whatever the OS has
 * since given that number to? A recorded PID proves nothing on its own — PIDs are
 * reused — so accept it only when the command line is the server we would have
 * launched.
 */
function isOurServer(pid: number): boolean {
  try {
    const cmd = execFileSync("ps", ["-o", "command=", "-p", String(pid)], {
      encoding: "utf8",
      timeout: 2000,
    });
    return cmd.includes(serverEntry());
  } catch {
    // No such process, or ps unavailable. Either way we have no grounds to kill.
    return false;
  }
}

/**
 * Kill a server left behind by a previous instance that did not shut down cleanly,
 * which would otherwise hold the deterministic port and push the next launch onto a
 * different MCP URL.
 */
export function reapOrphan(): void {
  let previous: Endpoint;
  try {
    previous = JSON.parse(readFileSync(userDataFile(), "utf8")) as Endpoint;
  } catch {
    return; // Clean shutdown last time, or nothing has ever run.
  }

  const pid = previous.serverPid;
  // Identity first, and only then the signal.
  if (typeof pid === "number" && pid > 1 && isOurServer(pid)) {
    try {
      // The group, as ever: the orphan may itself have spawned `octo` children.
      process.kill(process.platform === "win32" ? pid : -pid, "SIGKILL");
    } catch {
      // Not ours to kill, or already gone. Either way, stop trying.
    }
  }

  rmSync(userDataFile(), { force: true });
}
