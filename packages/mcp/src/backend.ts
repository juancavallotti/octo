/**
 * The backend port the MCP server is parameterized over. The two hosts store
 * integrations differently — the platform behind the orchestrator REST API, the
 * standalone app on local disk — so the reusable tool/resource/prompt layer
 * depends only on these injected capabilities, never on a concrete store. A host
 * supplies an {@link OctoMcpConfig}; run control is handled inside the package via
 * `@octo/run-host`, keyed by a per-MCP-session namespace.
 */

/** A stored integration: its id, display name, and runtime-YAML definition. */
export interface IntegrationRecord {
  id: string;
  name: string;
  /** The runtime YAML the `octo` binary loads (already a runnable config). */
  definition: string;
}

/**
 * CRUD over the host's integration store. Mirrors the host's existing data layer
 * (orchestrator client on platform, disk store on standalone) — the adapter is a
 * thin shim. `update` renames when `name` is given and its slug changes (the host
 * decides), returning the possibly-new record.
 */
export interface IntegrationStore {
  list(): Promise<{ id: string; name: string }[]>;
  get(id: string): Promise<IntegrationRecord>;
  create(name: string, definition: string): Promise<IntegrationRecord>;
  update(
    id: string,
    name: string | undefined,
    definition: string,
  ): Promise<IntegrationRecord>;
}

/** The outcome of validating a definition before a run. */
export interface ValidationOutcome {
  valid: boolean;
  errors: string[];
}

/** Everything a host injects to stand up the Octo MCP server. */
export interface OctoMcpConfig {
  /** The integration store backing list/open/create/update. */
  store: IntegrationStore;
  /**
   * Validate a stored definition (the host wraps `@octo/editor`'s document
   * validation). Used by `can_start_integration` before a run is attempted.
   */
  validate(definition: string): ValidationOutcome;
  /**
   * The runtime capability catalogue (blocks/connectors) served as the
   * `octo://runtime/schema` resource — `capabilities.json` from `@octo/editor`.
   */
  runtimeSchema: unknown;
  /**
   * Public origin used to absolutize a run's test path (e.g.
   * `http://localhost:3000`). When unset, the bare `/editor/runs/<ns>/` path is
   * returned and the consumer joins it with the app origin itself.
   */
  baseUrl?: string;
  /**
   * Base URL of the human documentation (CEL expression syntax, the block and
   * connector reference, connector configuration). When set, the
   * `create-integration` prompt points the LLM at it. Omit to leave it out.
   */
  docsUrl?: string;
}
