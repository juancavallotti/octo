import type { OctoEvent } from "./types";

/**
 * A lightweight in-process publish/subscribe bus shared across the BFF's route
 * handlers. Writers (the MCP store adapter) publish; the SSE route subscribes and
 * fans each event out to connected editors. State is a module-level Set, so it is
 * a true singleton only within a single Node server process — exactly like
 * @octo/run-host's log buffers. In a multi-replica deploy an event published on
 * one replica is not seen by subscribers on another; acceptable for the editor's
 * live-reload hint (and a non-issue for the single-process standalone app).
 */

type Listener = (event: OctoEvent) => void;

const listeners = new Set<Listener>();

/** Deliver an event to every current subscriber. */
export function publish(event: OctoEvent): void {
  for (const listener of listeners) {
    try {
      listener(event);
    } catch {
      // A listener whose stream has closed is harmless; it unsubscribes on cancel.
    }
  }
}

/** Subscribe to every event; returns an unsubscribe function. */
export function subscribe(fn: Listener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
