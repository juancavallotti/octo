import type { OctoEvent } from "./types";

/** The conventional route both apps mount {@link integrationEventStream} on. */
export const INTEGRATION_EVENTS_PATH = "/api/integrations/events";

/**
 * Subscribe to the BFF event stream from the browser: opens an EventSource to the
 * events route, parses each frame as an {@link OctoEvent}, and invokes `onEvent`.
 * Returns a function that closes the connection. Browser-only (uses EventSource).
 */
export function subscribeIntegrationEvents(
  onEvent: (event: OctoEvent) => void,
  path: string = INTEGRATION_EVENTS_PATH,
): () => void {
  const source = new EventSource(path);
  source.onmessage = (ev) => {
    try {
      onEvent(JSON.parse(ev.data) as OctoEvent);
    } catch {
      // Ignore malformed frames; keep-alive comments never reach onmessage.
    }
  };
  return () => source.close();
}
