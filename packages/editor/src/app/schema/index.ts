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
  Merge,
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
  Terminal,
  Box,
  ScatterChart,
  Leaf,
  type LucideIcon,
} from "lucide-react";
import { SlackIcon } from "./slack-icon";
import { NotionIcon } from "./notion-icon";
import { McpIcon } from "./mcp-icon";
import { PineconeIcon } from "./pinecone-icon";
import capsJson from "./capabilities.json";
import type {
  BlockSpec,
  Capabilities,
  ConnectorSpec,
  SourceSpec,
} from "./types";

/**
 * Loader for the runtime capability schema. The runtime is the source of truth:
 * it generates the full catalogue from its Go block/connector metadata
 * (`octo schema`), and a host that can reach the runner injects it at boot via
 * {@link setCapabilities}. The bundled JSON is only an *empty* barebones fallback
 * (`{blocks:[],connectors:[]}`) so the editor renders without crashing when no
 * runner is available — it no longer mirrors the runtime's catalogue. This module
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
 * The bundled capability schema — now an empty barebones fallback. Server-safe
 * hosts (MCP) resolve the real schema from the runner (`octo schema`) and fall
 * back to this empty catalogue only when no runner is configured.
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
  Merge,
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
  Terminal,
  ScatterChart,
  Slack: SlackIcon,
  Notion: NotionIcon,
  Mcp: McpIcon,
  Pinecone: PineconeIcon,
  // MongoDB has no lucide brand icon, and an approximated one would be
  // trademark artwork nobody verified. Leaf is the nearest honest stand-in —
  // it echoes the shape of MongoDB's own mark without claiming to be it.
  Leaf,
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
