import {
  Wand2,
  Variable,
  Trash2,
  ScrollText,
  Globe,
  Database,
  Webhook,
  ShieldCheck,
  GitFork,
  Split,
  Repeat,
  Clock,
  Box,
  type LucideIcon,
} from "lucide-react";
import capsJson from "./capabilities.json";
import type {
  BlockSpec,
  Capabilities,
  ConnectorSpec,
  SourceSpec,
} from "./types";

/**
 * Loader for the runtime capability schema. The JSON is the source of truth for
 * data; this module types it and resolves the icon names blocks reference to
 * actual lucide components (icons can't live in JSON).
 */
export const CAPABILITIES = capsJson as Capabilities;

const ICONS: Record<string, LucideIcon> = {
  Wand2,
  Variable,
  Trash2,
  ScrollText,
  Globe,
  Database,
  Webhook,
  ShieldCheck,
  GitFork,
  Split,
  Repeat,
  Clock,
};

/** Resolve a block's icon name to a component, falling back to a generic box. */
export function resolveIcon(name: string): LucideIcon {
  return ICONS[name] ?? Box;
}

export function listBlocks(): BlockSpec[] {
  return CAPABILITIES.blocks;
}

export function getBlockSpec(type: string): BlockSpec | undefined {
  return CAPABILITIES.blocks.find((b) => b.type === type);
}

export function listConnectors(): ConnectorSpec[] {
  return CAPABILITIES.connectors;
}

export function getConnectorSpec(type: string): ConnectorSpec | undefined {
  return CAPABILITIES.connectors.find((c) => c.type === type);
}

/** A source spec paired with the connector type/label that exposes it. */
export interface ListedSource {
  connector: string;
  connectorLabel: string;
  spec: SourceSpec;
}

/** Every source across all connectors, for the source picker. */
export function listSources(): ListedSource[] {
  return CAPABILITIES.connectors.flatMap((c) =>
    c.sources.map((spec) => ({
      connector: c.type,
      connectorLabel: c.label,
      spec,
    })),
  );
}

/** Resolve a source spec by its connector type and source type. */
export function getSourceSpec(
  connector: string,
  type: string,
): SourceSpec | undefined {
  return getConnectorSpec(connector)?.sources.find((s) => s.type === type);
}

export type { BlockSpec, ConnectorSpec, Capabilities } from "./types";
export type {
  FieldSpec,
  FieldType,
  SourceSpec,
  BlockCategory,
  ReferenceSpec,
} from "./types";
