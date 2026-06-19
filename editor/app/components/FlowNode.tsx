"use client";

import { type ReactNode } from "react";

/**
 * The schematic node shape shared by sources and steps: an icon in a bordered
 * circle that overlaps the left edge of a rounded label pill. Interactive
 * overlays (drag handle, remove) are passed as `children` and positioned by the
 * caller; the wrapper is a `group` so they can reveal on hover.
 */
export default function FlowNode({
  icon,
  label,
  sublabel,
  selected = false,
  children,
}: {
  icon: ReactNode;
  label: string;
  sublabel?: string;
  selected?: boolean;
  children?: ReactNode;
}) {
  const ring = selected
    ? "border-sky-500"
    : "border-zinc-800 dark:border-zinc-300";

  return (
    <div className="group relative inline-flex items-center">
      <div
        className={[
          "z-10 flex h-12 w-12 shrink-0 items-center justify-center rounded-full border-2 bg-white dark:bg-zinc-900 shadow-sm",
          ring,
        ].join(" ")}
      >
        {icon}
      </div>
      <div
        className={[
          "-ml-5 flex flex-col rounded-2xl border-2 bg-white dark:bg-zinc-900 py-2.5 pl-7 pr-5 shadow-sm",
          ring,
        ].join(" ")}
      >
        <span className="whitespace-nowrap text-sm font-semibold leading-tight">
          {label}
        </span>
        {sublabel && (
          <span className="whitespace-nowrap text-xs text-zinc-500 leading-tight">
            {sublabel}
          </span>
        )}
      </div>
      {children}
    </div>
  );
}
