/**
 * @octo/events — a lightweight in-process event bus for the BFF, plus the SSE
 * plumbing that carries its events to the browser. The MCP server publishes when
 * it writes an integration; the editor subscribes (via an EventSource on the
 * apps' /api/integrations/events route) and live-reloads the file it has open.
 * Isomorphic: the bus and stream helper run on the Node server, the subscribe
 * helper in the browser.
 */

export type { OctoEvent, IntegrationUpdatedEvent } from "./types";
export { publish, subscribe } from "./bus";
export { integrationEventStream } from "./stream";
export {
  subscribeIntegrationEvents,
  INTEGRATION_EVENTS_PATH,
} from "./client";
