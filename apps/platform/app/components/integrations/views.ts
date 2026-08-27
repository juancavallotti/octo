import {
  BrainCircuit,
  Database,
  KeyRound,
  LayoutDashboard,
  LayoutGrid,
  Network,
  Rocket,
  ScrollText,
  Waypoints,
} from "lucide-react";

/**
 * The top-level sections of the platform, each its own route. Kept in a plain
 * (non-"use client") module so both the server route pages and the client nav
 * import the real array, not a client-reference proxy. Each carries the icon the
 * nav renders next to its label (matching the dashboard shortcut icons). Rendered
 * in the shared header on every signed-in page so the bar stays put as you move
 * between sections.
 */
export const MANAGEMENT_SECTIONS = [
  {
    key: "dashboard",
    label: "Dashboard",
    href: "/platform",
    icon: LayoutDashboard,
  },
  {
    key: "integrations",
    label: "Integrations",
    href: "/platform/integrations",
    icon: LayoutGrid,
  },
  {
    key: "deployments",
    label: "Deployments",
    href: "/platform/deployments",
    icon: Rocket,
  },
  {
    key: "objects",
    label: "Object Store",
    href: "/platform/objects",
    icon: Database,
  },
  // Beside the Object Store, and that is the argument for where it sits: both are
  // what a running integration has written down rather than what someone
  // configured, and an operator reaches for them for the same reason.
  {
    key: "memory",
    label: "Agent memory",
    href: "/platform/memory",
    icon: BrainCircuit,
  },
  {
    key: "secrets",
    label: "Secrets",
    href: "/platform/secrets",
    icon: KeyRound,
  },
  { key: "queues", label: "Queues", href: "/platform/queues", icon: Network },
  { key: "logs", label: "Logs", href: "/platform/logs", icon: ScrollText },
  { key: "traces", label: "Traces", href: "/platform/traces", icon: Waypoints },
] as const;

export type ManagementSection = (typeof MANAGEMENT_SECTIONS)[number];
export type ManagementSectionKey = ManagementSection["key"];
