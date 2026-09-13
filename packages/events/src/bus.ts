import type { OctoEvent } from "./types";

/**
 * A lightweight in-process publish/subscribe bus: writers publish, subscribers are fanned
 * out to. State is a module-level Set, so it is a singleton only within one Node process
 * — an event published by one process reaches no subscriber in another, which makes this
 * a hint rather than a delivery guarantee.
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
