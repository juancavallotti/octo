"use client";

import { useCallback, useRef, useState } from "react";
import { Activity, Database } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import ObjectsManager from "./ObjectsManager";
import StorageHealth from "./StorageHealth";

/**
 * The two views of the object store: the objects themselves, and how full the
 * stores holding them are.
 *
 * A tab here rather than an entry in the section nav because the second question
 * only ever arrives out of the first — someone looking at a volatile object that is
 * not there wants to know whether Redis evicted it. Putting it in the top-level nav
 * would file it as a separate concern and make the two a page apart.
 *
 * The state is local rather than in the URL: the objects view already owns
 * `?deployment` and `?key`, and adding a third parameter that changes what those
 * two mean would make a shared link ambiguous.
 */
type Tab = "objects" | "storage";

const TABS: { id: Tab; label: string; icon: LucideIcon }[] = [
  { id: "objects", label: "Objects", icon: Database },
  { id: "storage", label: "Storage health", icon: Activity },
];

export default function ObjectsTabs() {
  const [tab, setTab] = useState<Tab>("objects");
  // Storage health polls every few seconds once mounted, so it is not mounted until
  // it is first selected — otherwise every visit to the object browser would open a
  // poll for a panel nobody looked at. Once opened it stays mounted, so switching
  // back does not restart it from an empty render.
  const [storageOpened, setStorageOpened] = useState(false);
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);

  const select = useCallback((next: Tab) => {
    setTab(next);
    if (next === "storage") setStorageOpened(true);
  }, []);

  /**
   * Arrow keys move between tabs, wrapping at both ends, and Home/End jump to the
   * outer ones — the keyboard model a tablist is expected to have. Combined with
   * the roving tabIndex below, the whole strip is one Tab stop: someone tabbing
   * through the page steps over it rather than through it, and moves inside it
   * with the arrows.
   */
  const onKeyDown = useCallback((e: React.KeyboardEvent, index: number) => {
    const last = TABS.length - 1;
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
    select(TABS[next].id);
    // Focus follows selection, which is what makes an automatic-activation
    // tablist usable: the arrow both moves and reveals, with no second keystroke.
    buttons.current[next]?.focus();
  }, [select]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div
        role="tablist"
        aria-label="Object store views"
        className="flex shrink-0 items-center gap-1 border-b border-black/10 px-4 dark:border-white/10"
      >
        {TABS.map(({ id, label, icon: Icon }, i) => (
          <button
            key={id}
            type="button"
            role="tab"
            id={`objects-tab-${id}`}
            ref={(el) => {
              buttons.current[i] = el;
            }}
            aria-selected={tab === id}
            aria-controls={`objects-panel-${id}`}
            // Roving tabIndex: only the selected tab is in the tab sequence.
            tabIndex={tab === id ? 0 : -1}
            onClick={() => select(id)}
            onKeyDown={(e) => onKeyDown(e, i)}
            className={`-mb-px flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm ${
              tab === id
                ? "border-zinc-900 font-medium text-zinc-900 dark:border-zinc-100 dark:text-zinc-100"
                : "border-transparent text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-200"
            }`}
          >
            <Icon size={14} aria-hidden />
            {label}
          </button>
        ))}
      </div>

      {/* The objects panel stays mounted so switching back does not re-fetch the
          list or lose the selected key. The storage panel is mounted on its first
          selection and kept from then on, for the same reason — it just does not
          pay for itself until somebody asks for it. */}
      <div
        role="tabpanel"
        id="objects-panel-objects"
        aria-labelledby="objects-tab-objects"
        hidden={tab !== "objects"}
        className={tab === "objects" ? "flex min-h-0 flex-1 flex-col" : "hidden"}
      >
        <ObjectsManager />
      </div>
      <div
        role="tabpanel"
        id="objects-panel-storage"
        aria-labelledby="objects-tab-storage"
        hidden={tab !== "storage"}
        className={tab === "storage" ? "min-h-0 flex-1 overflow-auto" : "hidden"}
      >
        {storageOpened ? <StorageHealth /> : null}
      </div>
    </div>
  );
}
