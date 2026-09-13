/**
 * @octo/events — a lightweight in-process event bus, plus the SSE plumbing that carries
 * its events to the browser. A writer publishes when it changes an integration, and a
 * subscriber reacts to the files it cares about. Isomorphic: the bus and stream helper
 * run on the Node server, the subscribe helper in the browser.
 */

export type {
  OctoEvent,
  IntegrationUpdatedEvent,
  TestSuitesUpdatedEvent,
  FlowMetaUpdatedEvent,
} from "./types";
export { publish, subscribe } from "./bus";
export { integrationEventStream } from "./stream";
export {
  subscribeIntegrationEvents,
  INTEGRATION_EVENTS_PATH,
} from "./client";
