import type { LucideIcon } from "lucide-react";
import { listBlocks, resolveIcon } from "../schema";

/** Group applied to blocks whose schema entry omits `group`. */
export const DEFAULT_GROUP = "Other";

export interface PaletteComponent {
  /** Block type, e.g. "log" — matches a schema BlockSpec and a model BlockNode. */
  id: string;
  label: string;
  icon: LucideIcon;
  /** Logical palette group (schema `group`, or {@link DEFAULT_GROUP}). */
  group: string;
}

/**
 * The palette of integration building blocks, derived from the runtime
 * capability schema (app/schema). Computed on demand (not at module load) so a
 * schema injected at boot via `setCapabilities` is reflected. Add blocks by
 * extending the runtime metadata / capabilities.json, not this file.
 */
export function palette(): PaletteComponent[] {
  return listBlocks().map((block) => ({
    id: block.type,
    label: block.label,
    icon: resolveIcon(block.icon),
    group: block.group ?? DEFAULT_GROUP,
  }));
}

/**
 * The groups present in the palette, in first-appearance (schema) order. The
 * Sidebar renders one collapsible section per group in this order; a group is
 * only listed once its first block is seen, so new blocks slot in without a
 * separate registry.
 */
export function paletteGroups(): string[] {
  return palette().reduce<string[]>((groups, c) => {
    if (!groups.includes(c.group)) groups.push(c.group);
    return groups;
  }, []);
}
