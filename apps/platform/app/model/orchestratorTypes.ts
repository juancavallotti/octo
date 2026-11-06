/**
 * The orchestrator's type surface: what a stored integration, snapshot, resource,
 * folder and deployment look like on the wire, plus the inputs the calls take.
 *
 * `orchestrator.ts` re-exports everything here, so `@/app/model/orchestrator`
 * remains the single import.
 */

/** A stored integration: a named flow definition (YAML) plus bookkeeping. */
export interface Integration {
  id: string;
  name: string;
  /**
   * An intentionally chosen icon, by name from the editor's icon registry.
   * Empty or absent means derive one from the definition.
   */
  icon?: string;
  /**
   * The integration's config files merged into one config — what a snapshot freezes
   * and a deployment runs. Read-only: the files are written one set at a time.
   */
  definition: string;
  /** RFC3339 timestamp of the last update. */
  lastUpdated: string;
  /**
   * Attribution: the ids of the creating and last-editing users, with those users
   * resolved to email/name for display. All optional — a row can have no actor, and
   * one whose user has since been deleted keeps the row and loses the name.
   */
  createdBy?: string;
  updatedBy?: string;
  createdByEmail?: string;
  createdByName?: string;
  updatedByEmail?: string;
  updatedByName?: string;
}

/** A version tag: a frozen snapshot of an integration, as the list names it. */
/** What promoting a version produced in the target environment. */
export interface Promotion {
  /** The target's integration: the one already there, or the one this created. */
  integrationId: string;
  /** Whether this promotion created that integration. */
  created: boolean;
  /** The target environment's name ("octo" for the default). */
  environment: string;
  snapshot: { id: string; integrationId: string; tag: string; createdAt: string };
}

export interface Snapshot {
  id: string;
  integrationId: string;
  tag: string;
  /** RFC3339 timestamp of when the tag was created. */
  createdAt: string;
}

/** One version tag with its frozen definition, read on its own. */
export interface SnapshotDetail extends Snapshot {
  definition: string;
}

/**
 * A resource frozen alongside a tag's definition — metadata only; content is
 * served separately, on demand, by the runtime's loader. Read-only.
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
  /**
   * Who wrote the file and who last changed it, resolved for display. Absent
   * when the write had no known actor — the MCP path, or local dev without SSO.
   */
  createdBy?: string;
  updatedBy?: string;
  createdByEmail?: string;
  createdByName?: string;
  updatedByEmail?: string;
  updatedByName?: string;
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

/** What the editor opens: an integration, its config files, and its folder. */
export interface IntegrationProject {
  integration: Integration;
  files: ConfigFile[];
  /** Null when the integration is unfiled. */
  folderId: string | null;
}

/**
 * The first view of an integration's detail pane, read in one go. A part that
 * could not be read is null, and its section reads it again itself, so one
 * failure costs one section rather than the pane.
 */
export interface IntegrationDetailData {
  snapshots: Snapshot[] | null;
  deployments: Deployment[] | null;
  resources: Resource[] | null;
  /** The working copy's declared env, from the deploy options. */
  envVars: DeployEnvVar[] | null;
}

/** Everything the integrations manager draws, read in one go. */
export interface IntegrationTree {
  folders: Folder[];
  integrations: Integration[];
  /** Folder id → the integrations it holds, in stored order. */
  members: Record<string, string[]>;
  /** The integrations with at least one deployment. */
  deployed: string[];
}

/**
 * Body for creating an integration. A non-empty definition becomes its one config
 * file; an empty one creates it with none, for a caller about to write its files.
 */
export interface IntegrationInput {
  name: string;
  definition: string;
}

/** Body for renaming an integration. Its config is written through its files. */
export interface IntegrationRename {
  name: string;
}

/**
 * One of an integration's config files: a root-level YAML file the runtime merges with
 * the others into one application.
 */
export interface ConfigFile {
  /** The file's name at the project root, e.g. `orders.yaml`. */
  path: string;
  content: string;
  /** RFC3339 timestamp of the last write, when known. */
  lastUpdated?: string;
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
  /** The deployment's persisted env bindings, keyed by var name. Secret bindings
   * carry only the secret name. */
  env?: Record<string, EnvBindingInput>;
  /** Whether this deployment's pods run with the runtime tracer on. */
  tracing?: boolean;
  /** The octo runtime image the pods are running, and its tag on its own. Absent
   * when neither the cluster nor the record knows. Not the platform's own version:
   * a deployment keeps the image it was created with until it is rolled over. */
  runtimeImage?: string;
  runtimeVersion?: string;
  /** The runtime release a rollout would move this deployment onto, present only
   * when it is newer than the one the pods run. */
  upgradeTo?: string;
  /** The runtime release the deployment is pinned to; absent when it follows the install. */
  runtimePin?: string;
  /** What this deployment's own token opens on the platform. See {@link DEPLOYMENT_ACCESS}. */
  access?: DeploymentAccess[];
  /** Which image its pods are: "" or "standard" for the default, "agentic" for the
   * heavier runner carrying a shell. */
  runner?: string;
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
   * What this deployment's own token opens beyond the stores every pod owns. See
   * {@link DEPLOYMENT_ACCESS}. Absent or empty opens nothing more.
   */
  access?: DeploymentAccess[];
  /** Which image the pods run: absent for the default, "agentic" for the runner with a shell. */
  runner?: string;
  /** Pin the deployment to a runtime release; absent or "latest" follows the install. */
  runtimeVersion?: string;
}

/** What a rollout changes. An absent field keeps the deployment's stored value. */
export interface RolloutChange {
  /** The version tag (snapshot id) to roll onto. */
  snapshotId: string;
  /** Replaces the stored env bindings wholesale. */
  env?: Record<string, EnvBindingInput>;
  tracing?: boolean;
  /** A runtime release to pin to, or "latest" to follow the install. */
  runtimeVersion?: string;
}

/** The runtime releases a deployment can be pinned to. */
export interface Runtimes {
  /** The release this install deploys; absent when it cannot say. */
  current?: string;
  /** Newest first: the current release and the supported ones no newer than it. */
  versions: string[];
}

/** The runtime value that follows the install instead of pinning a release. */
export const RUNTIME_LATEST = "latest";

/**
 * One thing a deployment's own token opens beyond its own stores.
 *
 * A running integration presents a token of its own, and what it may do is what
 * that token carries. For an integration woken by a queue message or a webhook
 * there is nobody else's credential to borrow, so this is the whole of what it
 * may reach. A set rather than a ladder: it can hold both, either, or neither.
 */
export type DeploymentAccess = "developer" | "operator";

/** The grants, with what each one is for. */
export const DEPLOYMENT_ACCESS: {
  value: DeploymentAccess;
  label: string;
  detail: string;
}[] = [
  {
    value: "developer",
    label: "Builds integrations",
    detail:
      "Reads and writes integrations, their resources, versions and dev runs — across the whole installation, not just its own. For an integration that builds or tests others.",
  },
  {
    value: "operator",
    label: "Operates deployments",
    detail:
      "Deploys, rolls out, scales and removes deployments — anyone else's included. For an integration that runs this platform.",
  },
];

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
   * (frozen for a tag, live for Current). A required var in this set counts as
   * satisfied, and can still be overridden with an explicit value or secret. */
  envProvidedKeys?: string[];
  /** Normalized form of the checked candidate. */
  slug?: string;
  /** The candidate has a usable form. */
  slugValid: boolean;
  /** The candidate is not already claimed (subdomain too, when external). */
  slugAvailable: boolean;
}
