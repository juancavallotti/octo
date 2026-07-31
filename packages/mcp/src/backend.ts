/**
 * The backend port the MCP server is parameterized over. The two hosts store
 * integrations differently — the platform behind the orchestrator REST API, the
 * standalone app on local disk — so the reusable tool/resource/prompt layer
 * depends only on these injected capabilities, never on a concrete store. A host
 * supplies an {@link OctoMcpConfig}; run control is handled inside the package via
 * `@octo/run-host`, keyed by a per-MCP-session namespace.
 */

import type { ResourceProvider } from "@octo/run-host";

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

/**
 * A stored integration resource: an external file the integration's config loads
 * at deploy/run time. `kind` is `env` (a `.env`-convention file merged into the
 * runtime environment) or `template` (a text template that may embed `{{ }}`
 * expressions). `name` is path-like (may contain `/`); a resource is addressed by
 * its opaque `id` within its owning integration, never by name.
 */
export interface ResourceRecord {
  id: string;
  integrationId: string;
  kind: "env" | "template";
  name: string;
  content: string;
}

/**
 * CRUD over one integration's resources, keyed by integration id (stateless — the
 * MCP layer holds no resource state). A thin, full-replace shim mirroring the
 * host's existing resource layer (nested `/integrations/{id}/resources` routes on
 * the platform, the flat disk store on standalone): `update` replaces the whole
 * record, so the tool layer fills any omitted `kind`/`name` from `get` first.
 */
export interface ResourceStore {
  list(integrationId: string): Promise<ResourceRecord[]>;
  get(integrationId: string, resourceId: string): Promise<ResourceRecord>;
  create(
    integrationId: string,
    kind: string,
    name: string,
    content: string,
  ): Promise<ResourceRecord>;
  update(
    integrationId: string,
    resourceId: string,
    kind: string,
    name: string,
    content: string,
  ): Promise<ResourceRecord>;
  remove(integrationId: string, resourceId: string): Promise<void>;
}

/**
 * Read and write the editor's own bookkeeping for one integration —
 * `.octo/editor-meta.json`, which holds the test inputs, block mocks and spies the
 * canvas shows.
 *
 * Raw content in both directions, exactly as the editor's own capability moves it: the
 * host decides where the file lives, and every bit of parsing stays in one place
 * (`@octo/editor/runtime`), so an agent's writes and the editor's reads cannot disagree
 * about the format.
 *
 * The file is design-time and *undeclared*: no config references it, so a deployed
 * runtime never pulls it and losing it costs only the debug setup.
 */
export interface MetaStore {
  /** The file's content, or "" when the integration has none yet. */
  load(integrationId: string): Promise<string>;
  save(integrationId: string, content: string): Promise<void>;
}

/**
 * Read and write an integration's dolphin test suites — one `<flow>_test.yaml` per flow.
 *
 * A different kind of file from {@link MetaStore}'s, and the difference is the point. The
 * meta file is design-time scratch that only the editor reads; a suite is a real,
 * committed artifact that CI and `dolphin test` in a terminal run and agree with. That is
 * what makes a test an agent leaves behind worth anything.
 */
export interface SuiteStore {
  /** Every suite stored against this integration, as { flow, content }. */
  list(integrationId: string): Promise<{ flow: string; content: string }[]>;
  /** Write one flow's suite, creating it when it is new. */
  save(integrationId: string, flow: string, content: string): Promise<void>;
  remove(integrationId: string, flow: string): Promise<void>;
}

/** The outcome of validating a definition before a run. */
export interface ValidationOutcome {
  valid: boolean;
  errors: string[];
}

/** Everything a host injects to stand up the Octo MCP server. */
/**
 * The runtime capability catalogue, or a (possibly async) resolver for it. A
 * function form lets the host generate the schema from the `octo` binary lazily
 * and cache it, rather than pinning a value at handler-construction time.
 */
export type RuntimeSchemaSource =
  | unknown
  | (() => unknown | Promise<unknown>);

/**
 * Resolve a {@link RuntimeSchemaSource} to its value: call it when it is a
 * function (awaiting a promise), otherwise return it as-is.
 */
export async function resolveRuntimeSchema(
  source: RuntimeSchemaSource,
): Promise<unknown> {
  return typeof source === "function"
    ? await (source as () => unknown | Promise<unknown>)()
    : source;
}

export interface OctoMcpConfig {
  /** The integration store backing list/open/create/update. */
  store: IntegrationStore;
  /**
   * Validate a stored definition (the host wraps `@octo/editor`'s document
   * validation). Used by `can_start_integration` before a run is attempted, and by
   * every mutating flow tool before it saves.
   *
   * May be async: validation is only as good as the capability catalogue behind it,
   * and a host generates that catalogue from the `octo` binary (see
   * {@link OctoMcpConfig.runtimeSchema}) — an async probe. A host that validates
   * against the editor's *bundled* fallback catalogue instead will find it empty and
   * report every block type unknown.
   */
  validate(definition: string): ValidationOutcome | Promise<ValidationOutcome>;
  /**
   * The runtime capability catalogue (blocks/connectors) served as the
   * `octo://runtime/schema` resource and the `getSchema` tool. The runtime is the
   * source of truth: a host passes a resolver that generates the schema from the
   * `octo` binary (`octo schema` via `@octo/run-host`), so MCP serves exactly what
   * the runner supports. May be a plain value or a (possibly async) function; a
   * resolver is called on each read, so it reflects a runner that appears later.
   */
  runtimeSchema: RuntimeSchemaSource;
  /**
   * Resolve the resources (env files, templates) a run's config declares, so
   * `@octo/run-host` can stage them for `run_integration`/`invoke_flow` — letting
   * a run read its credentials from the host's resource store instead of the
   * caller. Given the integration id (absent for an inline `invoke_flow`
   * definition), returns a provider bound to it, or undefined when the host can't
   * supply resources for it (e.g. the platform with no id). Omit the whole
   * capability on a host without resources.
   */
  resources?: (integrationId?: string) => ResourceProvider | undefined;
  /**
   * CRUD over an integration's stored resources (env files, templates), backing
   * the resource-management tools (`list_resources`, `open_resource`,
   * `create_resource`, `update_resource`, `delete_resource`). Distinct from
   * {@link OctoMcpConfig.resources}, which stages a run's resources for the run
   * host. Omit on a host without a resource store — the CRUD tools then aren't
   * registered (but `list_env_keys` still works from the definition alone).
   */
  resourceStore?: ResourceStore;
  /**
   * The editor's bookkeeping file, backing the flow-meta tools (`get_flow_meta`,
   * `set_test_input`, `set_mock`, `set_spy` and their deletes). Omit on a host that
   * keeps none — the tools then aren't registered, but `list_block_addresses` still
   * works from the definition alone.
   */
  metaStore?: MetaStore;
  /**
   * The integration's dolphin test suites, backing the test-authoring tools
   * (`list_test_suites`, `get_test_suite`, `set_test_suite`, `set_test_case`,
   * `delete_test_case`, `run_tests`). Omit on a host that keeps none.
   */
  suiteStore?: SuiteStore;
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
