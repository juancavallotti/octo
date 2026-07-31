/**
 * Events published on the in-process BFF event bus. A discriminated union so the
 * set can grow; today the only member is an integration-write notification.
 */

/** An integration's DEFINITION was created or updated (e.g. by the MCP server). */
export interface IntegrationUpdatedEvent {
  type: "integration.updated";
  /** Integration id — the orchestrator UUID, or the standalone flow filename. */
  id: string;
  /** Display name, for human-readable messages. */
  name: string;
}

/**
 * An integration's dolphin test suites changed.
 *
 * Its own event rather than an `integration.updated`, because the two ask the editor
 * for different things: the definition is the document, and reloading it over unsaved
 * edits needs the user's say-so. A suite is a separate file, and a write to one has no
 * business raising a banner about the flow — or, worse, prompting someone to discard
 * canvas work because an agent wrote a test.
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
