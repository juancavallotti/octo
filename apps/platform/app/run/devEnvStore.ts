import type { DevEnvStore } from "@octo/editor";
import { DEV_ENV_RESOURCE } from "@octo/run-host";
import { listResources, upsertResource } from "@/app/actions/resources";
import { unwrap } from "@/app/model/bff";

/**
 * The platform dev-env store: the editor's Dev .env panel reads and writes the
 * integration's `.env.dev` resource in the orchestrator, through the auth-gated
 * resource actions. A resource needs an owning integration, so editing is only
 * available once the integration is saved (an unsaved draft has no id).
 */
export const bffDevEnvStore: DevEnvStore = {
  async load(integrationId) {
    if (!integrationId) return "";
    const resources = unwrap(await listResources(integrationId));
    return resources.find((r) => r.name === DEV_ENV_RESOURCE)?.content ?? "";
  },
  async save(integrationId, content) {
    if (!integrationId) return; // canEdit gates this; nothing to persist for a draft
    unwrap(await upsertResource(integrationId, "env", DEV_ENV_RESOURCE, content));
  },
  canEdit(integrationId) {
    return !!integrationId;
  },
};
