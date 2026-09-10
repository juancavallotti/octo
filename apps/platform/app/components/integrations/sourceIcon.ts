import type { LucideIcon } from "lucide-react";
import { Workflow } from "lucide-react";
import { parse } from "yaml";
import {
  fromDefinitionYaml,
  getConnectorSpec,
  getSourceSpec,
  resolveIcon,
} from "@octo/editor/runtime";
import { DR_OCTO_AGENT_ID } from "@/app/agent/identity";

/**
 * Choose a scannable icon for an integration from its definition, so the list
 * reads by source type at a glance instead of every row sharing one glyph.
 *
 * The type is derived from the integration's source/connector types with a
 * priority order: the most distinctive, user-facing trigger wins. This matters
 * for Slack bots, whose transport is an http source but whose identity is Slack —
 * so a declared `slack` connector outranks the raw `http` entry. Notion webhooks
 * work the same way. Unknown or unparseable definitions fall back to the generic
 * Workflow glyph.
 */

// Most-distinctive first. A type present here is preferred over a later one (and
// over any other connector) when the integration declares several.
const TYPE_PRIORITY = ["slack", "notion", "cron", "events", "queue", "http"];

/**
 * Is this Dr. Octo? He is an integration like any other — an Octo App, which is
 * the whole joke — so he arrives here as a definition and would otherwise be drawn
 * as the http source he happens to ride on.
 *
 * Asked of the service name he declares, not of the integration's title: the name
 * is part of his definition (orchestrator/agent/config.yaml), while a title is
 * something anyone can type over.
 *
 * Read from the raw YAML rather than from the parsed document, because the parsed
 * one keeps only what the loaded capability schema knows about — and this runs in
 * places that have no schema, where the whole thing would silently stop working.
 */
function isDrOcto(definition: string): boolean {
  try {
    const doc = parse(definition) as { service?: { name?: unknown } } | null;
    return doc?.service?.name === DR_OCTO_AGENT_ID;
  } catch {
    return false;
  }
}

export function iconForDefinition(definition: string): LucideIcon {
  if (isDrOcto(definition)) return resolveIcon("DrOcto");

  let doc;
  try {
    doc = fromDefinitionYaml(definition);
  } catch {
    return Workflow;
  }

  // The connector types that trigger the integration: each flow's source, plus
  // every declared connector (captures Slack, which rides on an http source).
  const sourceTypes = doc.flows
    .map((f) => f.source?.connector)
    .filter((t): t is string => Boolean(t));
  const connectorTypes = doc.connectors.map((c) => c.type);
  const present = new Set([...sourceTypes, ...connectorTypes]);

  const primary =
    TYPE_PRIORITY.find((t) => present.has(t)) ??
    sourceTypes[0] ??
    connectorTypes[0];
  if (!primary) return Workflow;

  // Prefer the entry source's own icon when the primary is that source's
  // connector; otherwise the connector's icon. Fall back to the generic glyph.
  const source = doc.flows.find((f) => f.source?.connector === primary)?.source;
  const iconName =
    (source?.type ? getSourceSpec(primary, source.type)?.icon : undefined) ??
    getConnectorSpec(primary)?.icon;
  return iconName ? resolveIcon(iconName) : Workflow;
}

/**
 * The icon to show for an integration: the one it chose, or the one its
 * definition suggests.
 *
 * An unset icon derives, which is what every integration did before choosing was
 * possible — so this reads the same as iconForDefinition until somebody picks.
 */
export function iconForIntegration(
  icon: string | undefined,
  definition: string,
): LucideIcon {
  return icon ? resolveIcon(icon) : iconForDefinition(definition);
}
