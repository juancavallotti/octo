"use client";

/**
 * Choose an integration's icon, or hand it back to the derivation.
 *
 * The trigger is the icon itself, so the header gains no new chrome — the thing
 * you want to change is the thing you click.
 */

import { createElement, useMemo } from "react";
import type { LucideIcon } from "lucide-react";
import { listIcons, resolveIcon } from "@octo/editor/runtime";
import { AppPicker } from "../AppPicker";
import { iconForDefinition } from "./sourceIcon";

/** The sentinel for "no choice — follow the definition". */
const DERIVED = "";

interface IconChoice {
  /** The stored value: an icon name, or "" for derived. */
  name: string;
  label: string;
  icon: LucideIcon;
}

export interface IconPickerOptions {
  /** The integration's stored icon, or "" when it has not chosen one. */
  icon: string;
  /** Used to show what the derivation currently suggests. */
  definition: string;
  disabled?: boolean;
  onSelect: (icon: string) => void;
}

export default function IconPicker(options: IconPickerOptions) {
  const { icon, definition, disabled, onSelect } = options;

  const items = useMemo<IconChoice[]>(() => {
    // "Suggested" is pinned first and writes "", so choosing it clears the stored
    // name rather than freezing today's guess — an integration whose trigger
    // changes later still follows it.
    const suggested: IconChoice = {
      name: DERIVED,
      label: "Suggested",
      icon: iconForDefinition(definition),
    };
    return [
      suggested,
      ...listIcons().map((name) => ({
        name,
        label: name,
        icon: resolveIcon(name),
      })),
    ];
  }, [definition]);

  // Opening on the current appearance rather than on nothing: an integration
  // that has not chosen is showing the derived icon, so that is what is selected.
  const selected = items.find((i) => i.name === icon) ?? items[0];

  return (
    <AppPicker<IconChoice>
      items={items}
      selected={selected}
      onSelect={(choice) => {
        if (choice.name !== icon) onSelect(choice.name);
      }}
      toKey={(choice) => choice.name || "__derived__"}
      toText={(choice) => choice.label}
      renderValue={(choice) => createElement(choice.icon, { size: 16 })}
      renderRow={(choice) => (
        <span className="flex items-center gap-2">
          {createElement(choice.icon, { size: 16 })}
          <span className="truncate">{choice.label}</span>
        </span>
      )}
      label="Icon"
      placeholder="Search icons…"
      loading={disabled}
    />
  );
}
