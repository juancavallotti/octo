/**
 * Events published on the in-process BFF event bus. A discriminated union so the
 * set can grow; today the only member is an integration-write notification.
 */

/** An integration was created or updated (e.g. by the MCP server). */
export interface IntegrationUpdatedEvent {
  type: "integration.updated";
  /** Integration id — the orchestrator UUID, or the standalone flow filename. */
  id: string;
  /** Display name, for human-readable messages. */
  name: string;
}

export type OctoEvent = IntegrationUpdatedEvent;
