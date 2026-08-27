"use client";

import { useCallback, useState } from "react";
import { Activity, Database } from "lucide-react";
import { TabStrip, tabPanelProps, type TabDef } from "@/app/components/TabStrip";
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

const TABS: readonly TabDef<Tab>[] = [
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

  const select = useCallback((next: Tab) => {
    setTab(next);
    if (next === "storage") setStorageOpened(true);
  }, []);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <TabStrip
        tabs={TABS}
        selected={tab}
        onSelect={select}
        label="Object store views"
        idPrefix="objects"
      />

      {/* The objects panel stays mounted so switching back does not re-fetch the
          list or lose the selected key. The storage panel is mounted on its first
          selection and kept from then on, for the same reason — it just does not
          pay for itself until somebody asks for it. */}
      <div
        {...tabPanelProps("objects", "objects", tab === "objects")}
        className={tab === "objects" ? "flex min-h-0 flex-1 flex-col" : "hidden"}
      >
        <ObjectsManager />
      </div>
      <div
        {...tabPanelProps("objects", "storage", tab === "storage")}
        className={tab === "storage" ? "min-h-0 flex-1 overflow-auto" : "hidden"}
      >
        {storageOpened ? <StorageHealth /> : null}
      </div>
    </div>
  );
}
