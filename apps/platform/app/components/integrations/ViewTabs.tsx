"use client";

import { MANAGEMENT_VIEWS, type ManagementView } from "./views";

export { MANAGEMENT_VIEWS, type ManagementView };

/** A small segmented control switching the management page between its views. */
export default function ViewTabs({
  view,
  onChange,
}: {
  view: ManagementView;
  onChange: (view: ManagementView) => void;
}) {
  return (
    <div className="flex items-center gap-0.5 rounded-md bg-black/[0.04] p-0.5 dark:bg-white/[0.06]">
      {MANAGEMENT_VIEWS.map((v) => (
        <button
          key={v}
          type="button"
          onClick={() => onChange(v)}
          className={`rounded px-2.5 py-1 text-sm font-medium capitalize transition-colors ${
            view === v
              ? "bg-white text-zinc-900 shadow-sm dark:bg-zinc-700 dark:text-white"
              : "text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
          }`}
        >
          {v}
        </button>
      ))}
    </div>
  );
}
