import {
  Wand2,
  Variable,
  Trash2,
  ScrollText,
  Globe,
  Database,
  Webhook,
  ShieldAlert,
  ShieldCheck,
  GitFork,
  Split,
  Repeat,
  Clock,
  Sparkles,
  RefreshCw,
  Route,
  Bot,
  BrainCog,
  Eraser,
  HardDriveDownload,
  HardDriveUpload,
  Send,
  Inbox,
  Layers,
  Radio,
  FileText,
  Box,
  type LucideIcon,
} from "lucide-react";
import { SlackIcon } from "./slack-icon";
import { NotionIcon } from "./notion-icon";
import { McpIcon } from "./mcp-icon";
import capsJson from "./capabilities.json";
import type {
  BlockSpec,
  Capabilities,
  ConnectorSpec,
  SourceSpec,
} from "./types";

/**
 * Loader for the runtime capability schema. The bundled JSON is the built-in
 * fallback; the source of truth is the runtime, which can generate the schema
 * from its Go block/connector metadata (`octo schema`). A host that can reach the
 * runner injects the generated schema at boot via {@link setCapabilities}; until
 * then (or when the runner is unavailable) the bundled JSON is used. This module
 * also resolves the icon names blocks reference to actual lucide components (icons
 * can't live in JSON).
 */
const FALLBACK = capsJson as Capabilities;

/** The active schema, swappable at runtime via {@link setCapabilities}. */
let active: Capabilities = FALLBACK;

/**
 * Replace the active capability schema (e.g. with one the runtime generated).
 * Idempotent and synchronous; call before the editor first renders so the palette
 * and settings forms derive from the injected schema. A null/undefined value
 * restores the bundled fallback.
 */
export function setCapabilities(caps: Capabilities | null | undefined): void {
  active = caps ?? FALLBACK;
}

/** The active capability schema (generated-and-injected, or the bundled fallback). */
export function getCapabilities(): Capabilities {
  return active;
}

/**
 * The bundled capability schema. Server-safe hosts (MCP) that don't inject a
 * generated schema serve this directly.
 */
export const CAPABILITIES = FALLBACK;

const ICONS: Record<string, LucideIcon> = {
  Wand2,
  Variable,
  Trash2,
  ScrollText,
  Globe,
  Database,
  Webhook,
  ShieldAlert,
  ShieldCheck,
  GitFork,
  Split,
  Repeat,
  Clock,
  Sparkles,
  RefreshCw,
  Route,
  Bot,
  BrainCog,
  Eraser,
  HardDriveDownload,
  HardDriveUpload,
  Send,
  Inbox,
  Layers,
  Radio,
  FileText,
  Slack: SlackIcon,
  Notion: NotionIcon,
  Mcp: McpIcon,
};

/** Resolve a block's icon name to a component, falling back to a generic box. */
export function resolveIcon(name: string): LucideIcon {
  return ICONS[name] ?? Box;
}

export function listBlocks(): BlockSpec[] {
  return active.blocks;
}

export function getBlockSpec(type: string): BlockSpec | undefined {
  return active.blocks.find((b) => b.type === type);
}

export function listConnectors(): ConnectorSpec[] {
  return active.connectors;
}

export function getConnectorSpec(type: string): ConnectorSpec | undefined {
  return active.connectors.find((c) => c.type === type);
}

/** A source spec paired with the connector type/label that exposes it. */
export interface ListedSource {
  connector: string;
  connectorLabel: string;
  spec: SourceSpec;
}

/** Every source across all connectors, for the source picker. */
export function listSources(): ListedSource[] {
  return active.connectors.flatMap((c) =>
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
