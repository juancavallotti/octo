import {
  Activity,
  BrainCircuit,
  Database,
  KeyRound,
  LayoutDashboard,
  LayoutGrid,
  Network,
  ScrollText,
  Waypoints,
  type LucideIcon,
} from "lucide-react";
import type { Capabilities } from "@/app/auth/capabilities";

/**
 * The top-level sections of the platform, each its own route. Kept in a plain
 * (non-"use client") module so both the server route pages and the client nav
 * import the real array, not a client-reference proxy. Each carries the icon the
 * nav renders next to its label (matching the dashboard shortcut icons). Rendered
 * in the shared header on every signed-in page so the bar stays put as you move
 * between sections.
 *
 * `requires` names the capability a section is worthless without — a page whose
 * every request would be refused. Sections with none are open to anyone signed
 * in, which is most of them: reading what is running here is not a privilege.
 */
export const MANAGEMENT_SECTIONS: readonly {
  key: string;
  label: string;
  href: string;
  icon: LucideIcon;
  requires?: keyof Capabilities;
}[] = [
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
    // Deployments, but named for what a reader comes here to find out. The card
    // for each one now carries the last five minutes of its CPU and memory and
    // links through to the rest, so the page answers "how is it going" before it
    // answers "what is running".
    key: "metrics",
    label: "Metrics",
    href: "/platform/metrics",
    icon: Activity,
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
    // The whole page is administrators', reads included: these are the
    // installation's own credentials, and its list of them is as telling as their
    // values.
    requires: "administer",
  },
  { key: "queues", label: "Queues", href: "/platform/queues", icon: Network },
  { key: "logs", label: "Logs", href: "/platform/logs", icon: ScrollText },
  { key: "traces", label: "Traces", href: "/platform/traces", icon: Waypoints },
];
