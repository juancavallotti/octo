import { settings } from "../settings";
import { locate, probe, type DockerStatus } from "./docker";

/**
 * The last known docker status, which the menu and the Container window are drawn
 * from. Refreshed rather than watched: there is no event for a daemon starting, so we
 * ask again whenever the user comes back to the app — the moment they would have
 * started Docker in between.
 */

let last: DockerStatus = { state: "missing" };
/** The probe running now, and the CLI it is asking. */
let inflight: { file: string | null; probe: Promise<DockerStatus> } | null = null;
const listeners = new Set<(status: DockerStatus) => void>();

export function dockerStatus(): DockerStatus {
  return last;
}

/** The CLI to run, or null when there is none to run. */
export function dockerCli(): string | null {
  return last.state === "missing" ? null : (last.path ?? null);
}

/** Be told when the status changes — not on every probe, most of which change nothing. */
export function onDockerStatus(listener: (status: DockerStatus) => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/**
 * Probe again. Concurrent callers asking the same CLI share one probe: focus events
 * arrive in bursts, and each probe is a process. A probe of a CLI the user has since
 * replaced in Settings is superseded — its answer describes a docker no longer in use,
 * so it is neither recorded nor announced.
 */
export function refreshDocker(): Promise<DockerStatus> {
  const file = locate(settings().docker);
  if (inflight?.file === file) return inflight.probe;
  const current: { file: string | null; probe: Promise<DockerStatus> } = {
    file,
    probe: probe(file).then((next) => {
      if (inflight !== current) return next;
      inflight = null;
      const changed = JSON.stringify(next) !== JSON.stringify(last);
      last = next;
      if (changed) for (const listener of listeners) listener(next);
      return next;
    }),
  };
  inflight = current;
  return current.probe;
}
