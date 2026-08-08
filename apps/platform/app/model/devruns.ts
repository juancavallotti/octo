/**
 * The dev-run wire shapes: the orchestrator's account of an app the editor is
 * running as a pod of its own (`orchestrator/internal/devrun`).
 *
 * Server-only, unlike most of this layer — nothing in the browser addresses a dev run.
 * The editor drives it through the RUN transport, which reaches the remote app runner
 * (`app/run/remoteRunner.ts`); these are the types that runner and the typed client
 * speak. They live here regardless, because this is where the orchestrator's shapes are
 * declared and a second home for a wire contract is how two copies of it start to
 * disagree.
 *
 * What is NOT here is as informative as what is. There is no port (a dev pod owns its
 * network namespace, so the listen port is a platform constant), no namespace (a dev run
 * is addressed by its derived id), and no "stopped" state: a dev run's existence IS its
 * status, so the absence of one from a list is the whole answer.
 */

/** A dev run's phase, as the orchestrator computes it from the workload and its pods. */
export type DevRunPhase = "pending" | "running" | "failed";

/**
 * One live dev run. Every field is read back from the cluster or re-derived from
 * (user, integration) — none of it is stored, so none of it can be stale in the way a
 * mirrored table would be.
 */
export interface DevRun {
  /** Derived from (user, integration) by the orchestrator; nothing mints or stores it. */
  id: string;
  userId: string;
  integrationId: string;
  status: DevRunPhase;
  ready: boolean;
  /** Why it is not ready, when the cluster can say — an image it cannot pull, no node. */
  reason?: string;
  /** Total restarts across the run's pods: how a crash loop shows up in a phase-only view. */
  restarts?: number;
  /** The stable public hostname. Absent for an integration that serves no HTTP. */
  host?: string;
  /** `host` as a URL — the address the editor offers. Absent for the same reason. */
  testUrl?: string;
  /** RFC3339. */
  createdAt?: string;
  /** RFC3339 of the last reload; what the orchestrator's idle reaper reads. */
  lastActivity?: string;
  /**
   * The sidecar's own view of the run (which generation it applied, when it last pulled),
   * passed through as the sidecar produced it. Absent while a pod is still starting.
   *
   * Deliberately opaque: the orchestrator does not interpret it either, and re-declaring
   * a schema owned by a separately versioned module is how a status surface starts lying.
   */
  sidecar?: unknown;
}

/**
 * What a Run reports: the dev run, plus whether this call started it or attached to one
 * that was already there. Two tabs on one integration share a pod by design, so an
 * attach is a normal outcome and not a hidden one.
 */
export interface EnsuredDevRun extends DevRun {
  created: boolean;
}
