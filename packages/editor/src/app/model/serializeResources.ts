import type { Resources, TemplateResource } from "./document";

/**
 * Maps the document's declared resources between the editor model and the runtime
 * config's top-level `resources:` section (see runtime/types/runtime.go). Split out
 * of serialize.ts like serializeEnv, to keep that file focused. `env` lists the
 * `.env`-convention files combined into the environment; `templates` declares the
 * template files rendered by the `template-resource` block / `templateResource()`.
 */

export interface RuntimeTemplateResource {
  resource?: string;
  as?: string;
}

export interface RuntimeResources {
  env?: string[];
  templates?: RuntimeTemplateResource[];
}

/**
 * Map the document's resources to the runtime shape, dropping empties so a
 * document with no resources emits no `resources:` key at all. Returns undefined
 * when nothing is declared.
 */
export function resourcesToRuntime(
  r: Resources | undefined,
): RuntimeResources | undefined {
  if (!r) return undefined;
  const out: RuntimeResources = {};
  const env = r.env.map((e) => e.trim()).filter((e) => e !== "");
  if (env.length) out.env = env;
  const templates = r.templates
    .filter((t) => t.resource.trim() !== "")
    .map((t) => {
      const o: RuntimeTemplateResource = { resource: t.resource.trim() };
      if (t.as && t.as.trim() !== "") o.as = t.as.trim();
      return o;
    });
  if (templates.length) out.templates = templates;
  return out.env || out.templates ? out : undefined;
}

/** Map the runtime `resources:` section back to the document model. */
export function resourcesFromRuntime(
  r: RuntimeResources | undefined,
): Resources {
  return {
    env: r?.env ? [...r.env] : [],
    templates: (r?.templates ?? [])
      .filter((t) => (t.resource ?? "") !== "")
      .map((t) => {
        const o: TemplateResource = { resource: t.resource ?? "" };
        if (t.as) o.as = t.as;
        return o;
      }),
  };
}
