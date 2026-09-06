/**
 * The orchestrator's type surface: what a stored integration, snapshot, resource,
 * folder and deployment look like on the wire, plus the inputs the calls take.
 *
 * Split out of `orchestrator.ts` because it is the bulk of that module and shares
 * nothing with the rest of it: these are shapes, and the other half is a list of
 * one-line calls. `orchestrator.ts` re-exports everything here, so
 * `@/app/model/orchestrator` remains the single import for both.
 */

/** A stored integration: a named flow definition (YAML) plus bookkeeping. */
export interface Integration {
  id: string;
  name: string;
  /** The flow definition, as the runtime YAML the editor serializes. */
  definition: string;
  /** RFC3339 timestamp of the last update. */
  lastUpdated: string;
  /**
   * Attribution: the ids of the creating and last-editing users, with those
   * users resolved to email/name for display. All optional — a row may have no
   * known actor (local no-SSO, MCP writes) or the user may since be gone.
   */
  createdBy?: string;
  updatedBy?: string;
  createdByEmail?: string;
  createdByName?: string;
  updatedByEmail?: string;
  updatedByName?: string;
}

/** A version tag: a frozen snapshot of an integration's definition. */
export interface Snapshot {
  id: string;
  integrationId: string;
  tag: string;
  /** The frozen definition YAML, for version-scoped stats (e.g. the Definition panel). */
  definition: string;
  /** RFC3339 timestamp of when the tag was created. */
  createdAt: string;
}

/**
 * A resource frozen alongside a tag's definition — metadata only (content is
 * served separately, on demand, by the runtime's loader). Read-only, since a
 * snapshot is immutable.
 */
export interface SnapshotResource {
  /** "env" or "template". */
  kind: string;
  name: string;
  /** RFC3339 timestamp of when the tag froze this resource. */
  createdAt: string;
}

/** An integration resource: an env file or text template the runtime loads. */
export interface Resource {
  id: string;
  integrationId: string;
  /** "env" or "template". */
  kind: string;
  /** The path-like id the config references (e.g. ".env.dev", "templates/welcome.tmpl"). */
  name: string;
  content: string;
  /** RFC3339 timestamps. */
  createdAt: string;
  lastUpdated: string;
}

/** An authenticated principal, provisioned from the OIDC identity on first sign-in. */
export interface User {
  /** The durable orchestrator id; the stable handle per-user data is scoped by. */
  id: string;
  email: string;
  name: string;
  /** RFC3339 timestamp of when the user was first provisioned. */
  createdAt: string;
  /** RFC3339 timestamp of the most recent sign-in. */
  lastLoginAt: string;
}

/** A folder in the single-membership organization tree. */
export interface Folder {
  id: string;
  parentId: string | null;
  name: string;
  /** Present on the tree returned by `listFolders`; nested children. */
  children?: Folder[];
}

/** Body for creating/updating an integration. */
export interface IntegrationInput {
  name: string;
  definition: string;
}

/** Coarse lifecycle status of a deployment, cached from the live cluster. */
export type DeploymentStatus = "pending" | "running" | "failed";

/** Live state of one runtime pod backing a deployment. */
export interface PodStatus {
  name: string;
  /** Pending/Running/Succeeded/Failed/Unknown. */
  phase: string;
  ready: boolean;
  restarts: number;
}

/** One deployed instance of an integration running as its own workload. */
export interface Deployment {
  id: string;
  integrationId: string;
  /** Display name, captured from the integration at deploy time. */
  name: string;
  /** The version tag this deployment was created from; absent on legacy deployments. */
  tag?: string;
  /** Cached lifecycle status; refreshed by the orchestrator on read. */
  status: DeploymentStatus;
  /** Desired/served replica count (from settings). */
  replicas: number;
  /** Ready replica count, live from the cluster. */
  readyReplicas: number;
  /** Desired replica count, live from the cluster's Deployment spec. */
  desiredReplicas: number;
  /** Terminal failure reason (e.g. ImagePullBackOff), when failed. */
  reason?: string;
  /** Per-pod live detail. */
  pods?: PodStatus[];
  /** In-cluster address other flows use to reach this integration, if any. */
  internalUrl?: string;
  /** Public https URL when the deployment is exposed externally. */
  externalUrl?: string;
  /** RFC3339 timestamp of the workload's creation (age anchor), if known. */
  createdAt?: string;
  /** RFC3339 timestamp of the last status/state update. */
  lastUpdated: string;
  /** The deployment's persisted env bindings, keyed by var name — for a rollout
   * dialog to seed "edit existing". Secret bindings carry only the secret name. */
  env?: Record<string, EnvBindingInput>;
  /** Whether this deployment's pods run with the runtime tracer on. */
  tracing?: boolean;
  /** The octo runtime image the pods are running, and its tag on its own. Absent
   * when neither the cluster nor the record knows. It is not the platform's own
   * version: a deployment keeps the image it was created with until it is rolled
   * over, so a cluster commonly runs several at once. */
  runtimeImage?: string;
  runtimeVersion?: string;
}

/** How one declared env var is filled at deploy: a literal value or a secret ref. */
export interface EnvBindingInput {
  value?: string;
  secret?: string;
}

/** Per-deployment options sent when deploying an integration. */
export interface DeploymentInput {
  /** The version tag (snapshot id) to deploy; required by the orchestrator. */
  snapshotId?: string;
  /** Runtime replicas; omitted/<=0 means a single replica. */
  replicas?: number;
  /** User-chosen internal address slug; omitted asks the orchestrator to allocate. */
  slug?: string;
  /** "external" publishes a {slug}.{baseDomain} endpoint with TLS. */
  expose?: "external";
  /** External host label; defaults to the slug when omitted. */
  subdomain?: string;
  /** Bindings for the integration's declared env vars, keyed by var name. */
  env?: Record<string, EnvBindingInput>;
  /** Run the pods with the runtime tracer on. Off by default; it costs throughput. */
  tracing?: boolean;
  /**
   * Declares that this deployment's flows call the orchestrator's own API. Grants
   * nothing today — ORCHESTRATOR_URL is already in every pod for the runtime's KV
   * store — but it is the declaration a future access model gates on.
   */
  orchestratorApi?: boolean;
  /**
   * Grants this deployment the observability service's address as OBSERVABILITY_URL. Off by
   * default: stored logs and traces span every deployment on the installation.
   */
  observabilityApi?: boolean;
}

/** An environment variable an integration declares, for the modal to prompt on. */
export interface DeployEnvVar {
  name: string;
  default?: string;
  required?: boolean;
}

/**
 * Deploy choices for an integration, backing the deploy modal. When fetched with a
 * candidate slug the `slug*` fields validate it (for the requested exposure);
 * otherwise `suggestedSlug` carries a free default to prefill.
 */
export interface DeployOptions {
  /** Whether the integration has an HTTP source (so it gets a slug and can expose). */
  networked: boolean;
  /** A free slug to prefill the field with (only when no candidate was checked). */
  suggestedSlug?: string;
  /** The integration's declared env vars (excluding orchestrator-managed ones). */
  envVars?: DeployEnvVar[];
  /** Env var names already supplied by the selected version's .env resources
   * (frozen for a tag, live for Current). A required var in this set is treated as
   * satisfied — the modal neither blocks nor forces a value, but it can be
   * overridden with an explicit value or secret. */
  envProvidedKeys?: string[];
  /** Normalized form of the checked candidate. */
  slug?: string;
  /** The candidate has a usable form. */
  slugValid: boolean;
  /** The candidate is not already claimed (subdomain too, when external). */
  slugAvailable: boolean;
}
