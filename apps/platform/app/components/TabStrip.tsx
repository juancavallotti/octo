"use client";

import { useCallback, useRef } from "react";
import type { LucideIcon } from "lucide-react";

/** One tab: what it is called, what identifies it, and what it looks like. */
export interface TabDef<Id extends string> {
  id: Id;
  label: string;
  icon?: LucideIcon;
}

/**
 * A tablist with the keyboard model a tablist is expected to have.
 *
 * Extracted because the object store grew one and the memory viewer needed the
 * same thing, and the part worth sharing is not the markup — it is the behaviour
 * underneath it. Arrow keys move between tabs and wrap at both ends, Home and End
 * jump to the outer ones, and a roving tabIndex makes the whole strip a single Tab
 * stop: someone tabbing through the page steps over it rather than through it, and
 * moves inside it with the arrows. Focus follows selection, which is what makes an
 * automatic-activation tablist usable — the arrow both moves and reveals, with no
 * second keystroke.
 *
 * Written twice, that behaviour drifts, and the copy that drifts is the one nobody
 * is looking at.
 *
 * The strip renders the buttons and nothing else. Panels stay with the caller,
 * because whether a panel is mounted when it is not selected is a decision about
 * that panel — one that polls should not be mounted until it is asked for, one
 * holding a selection should not be unmounted once it has been.
 */
export function TabStrip<Id extends string>({
  tabs,
  selected,
  onSelect,
  label,
  idPrefix,
}: {
  tabs: readonly TabDef<Id>[];
  selected: Id;
  onSelect: (id: Id) => void;
  /** Names the strip for a screen reader: "Object store views". */
  label: string;
  /** Namespaces the DOM ids, so two strips on one page do not collide. */
  idPrefix: string;
}) {
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent, index: number) => {
      const last = tabs.length - 1;
      let next: number;
      switch (e.key) {
        case "ArrowRight":
          next = index === last ? 0 : index + 1;
          break;
        case "ArrowLeft":
          next = index === 0 ? last : index - 1;
          break;
        case "Home":
          next = 0;
          break;
        case "End":
          next = last;
          break;
        default:
          return;
      }
      e.preventDefault();
      onSelect(tabs[next].id);
      buttons.current[next]?.focus();
    },
    [onSelect, tabs],
  );

  return (
    <div
      role="tablist"
      aria-label={label}
      className="flex shrink-0 items-center gap-1 border-b border-black/10 px-4 dark:border-white/10"
    >
      {tabs.map(({ id, label: text, icon: Icon }, i) => (
        <button
          key={id}
          type="button"
          role="tab"
          id={`${idPrefix}-tab-${id}`}
          ref={(el) => {
            buttons.current[i] = el;
          }}
          aria-selected={selected === id}
          aria-controls={`${idPrefix}-panel-${id}`}
          tabIndex={selected === id ? 0 : -1}
          onClick={() => onSelect(id)}
          onKeyDown={(e) => onKeyDown(e, i)}
          className={`-mb-px flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm ${
            selected === id
              ? "border-zinc-900 font-medium text-zinc-900 dark:border-zinc-100 dark:text-zinc-100"
              : "border-transparent text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-200"
          }`}
        >
          {Icon && <Icon size={14} aria-hidden />}
          {text}
        </button>
      ))}
    </div>
  );
}

/** The props a panel needs to be addressed by its tab. Keeps the ids in step. */
export function tabPanelProps(idPrefix: string, id: string, selected: boolean) {
  return {
    role: "tabpanel" as const,
    id: `${idPrefix}-panel-${id}`,
    "aria-labelledby": `${idPrefix}-tab-${id}`,
    hidden: !selected,
  };
}
