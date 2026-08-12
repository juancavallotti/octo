/**
 * Where Dr. Octo may take you.
 *
 * Sent with every message rather than written into the agent's prompt, because the
 * app that owns these routes is the only thing that knows them: a list baked into
 * the agent's definition — which ships inside the orchestrator binary — would go
 * stale the first time a page was added here.
 *
 * A `{placeholder}` is a template the agent fills in from something it looked up.
 * Keep the descriptions short and factual; they are read by a model deciding
 * whether a page answers the question it was asked, not by a person browsing.
 */

export interface RouteHint {
  path: string;
  description: string;
}

export const ROUTE_CATALOGUE: readonly RouteHint[] = [
  { path: "/platform", description: "The dashboard: shortcuts to everything below." },
  { path: "/platform/integrations", description: "Every integration, as a file tree." },
  {
    path: "/platform/i/{integrationId}",
    description: "One integration in the editor: its flows, resources and tests.",
  },
  { path: "/platform/deployments", description: "Running deployments, their pods and status." },
  { path: "/platform/traces", description: "Traces of flow runs, when tracing is on." },
  { path: "/platform/logs", description: "Logs across deployments." },
  { path: "/platform/queues", description: "Queues and their pending messages." },
  { path: "/platform/objects", description: "The KV store's objects, by namespace." },
  { path: "/platform/secrets", description: "Cluster secrets available to deployments." },
  { path: "/platform/new", description: "Create a new integration." },
  { path: "/platform/account", description: "The signed-in user's own API keys." },
  { path: "/platform/admin", description: "Site-wide settings for this installation." },
  { path: "/platform/admin/email", description: "The email provider the platform sends through." },
  {
    path: "/platform/admin/llm",
    description: "The LLM provider, model and API key the platform agent reasons with.",
  },
  {
    path: "/platform/admin/agent",
    description: "Install, update, trace or remove the platform agent.",
  },
] as const;
