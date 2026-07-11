import type { EditorMetaStore } from "@octo/editor";
import { listResources, upsertResource } from "@/app/actions/resources";
import { unwrap } from "@/app/model/bff";

/**
 * The editor-meta resource name (mirrors the editor's EDITOR_META_RESOURCE). Inlined
 * rather than imported so this client-reachable module doesn't drag the editor barrel
 * into the browser bundle, the same reason bffDevEnvStore inlines `.env.dev`.
 */
const EDITOR_META_RESOURCE = ".octo/editor-meta.json";

/**
 * The platform editor-meta store: the integration's `.octo/editor-meta.json` resource in
 * the orchestrator, read and written through the auth-gated resource actions — the same
 * path the Dev .env panel takes.
 *
 * It is stored with kind `template` because that is what the orchestrator's resource
 * model offers for a non-env file; nothing renders it. Being *undeclared* is what keeps
 * it out of the way: a deployed runtime pulls resources by name, and only the ones its
 * config declares, so nothing ever asks for this one — and the Resources view hides
 * anything under `.octo/`, so it stays out of the user's file list too.
 *
 * A resource needs an owning integration, so editing is only available once the
 * integration is saved (a draft has no id).
 */
export const bffEditorMetaStore: EditorMetaStore = {
  async load(integrationId) {
    if (!integrationId) return "";
    const resources = unwrap(await listResources(integrationId));
    return resources.find((r) => r.name === EDITOR_META_RESOURCE)?.content ?? "";
  },
  async save(integrationId, content) {
    if (!integrationId) return; // canEdit gates this; a draft has nothing to key by
    unwrap(await upsertResource(integrationId, "template", EDITOR_META_RESOURCE, content));
  },
  canEdit(integrationId) {
    return !!integrationId;
  },
};
