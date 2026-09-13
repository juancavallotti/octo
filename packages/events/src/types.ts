/**
 * Events published on the in-process bus. A discriminated union, so the set can grow.
 */

/** An integration's DEFINITION was created or updated (e.g. by the MCP server). */
export interface IntegrationUpdatedEvent {
  type: "integration.updated";
  /** Integration id, in whatever form the publishing host addresses one by. */
  id: string;
  /** Display name, for human-readable messages. */
  name: string;
}

/**
 * An integration's dolphin test suites changed.
 *
 * Its own event rather than an `integration.updated`, because the two ask a subscriber
 * for different things: reloading the document over unsaved edits needs the user's say-so,
 * while a suite is a separate file whose write should raise nothing about the flow.
 */
export interface TestSuitesUpdatedEvent {
  type: "integration.tests-updated";
  id: string;
}

/** An integration's editor bookkeeping — its test inputs, mocks and spies — changed. */
export interface FlowMetaUpdatedEvent {
  type: "integration.meta-updated";
  id: string;
}

export type OctoEvent =
  | IntegrationUpdatedEvent
  | TestSuitesUpdatedEvent
  | FlowMetaUpdatedEvent;
