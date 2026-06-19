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
  Box,
  type LucideIcon,
} from "lucide-react";
import capsJson from "./capabilities.json";
import type { BlockSpec, Capabilities, ConnectorSpec } from "./types";

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

export function getConnectorSpec(type: string): ConnectorSpec | undefined {
  return CAPABILITIES.connectors.find((c) => c.type === type);
}

export type { BlockSpec, ConnectorSpec, Capabilities } from "./types";
export type { FieldSpec, FieldType, SourceSpec, BlockCategory } from "./types";
