"use client";

import { useState } from "react";
import { Layers } from "lucide-react";
import DefaultPill from "../platform/components/DefaultPill";
import type { EnvironmentView } from "../platform/types";

/**
 * A cloud profile's mark. Drawn here rather than taken from lucide, whose cloud is
 * lopsided enough to read as a smudge at heading size.
 */
function CloudMark({ size = 13 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2.2}
      strokeLinejoin="round"
      aria-hidden="true"
      className="shrink-0"
    >
      <path d="M6.5 19.5h11a4.25 4.25 0 0 0 .55-8.46A6.25 6.25 0 0 0 6.1 9.6 4.98 4.98 0 0 0 6.5 19.5Z" />
    </svg>
  );
}

/**
 * The folder menu's heading over an installation's integrations: whose they are, in
 * which environment, and a way to pick another.
 *
 * The environment is the Platform window's chip, the same icon and look, but its
 * choices unfold under the heading instead of in a menu of their own: the heading
 * lives inside the folder menu's scrolling list, which would clip a second popover.
 * A single environment is said, not offered. The default is marked as the platform
 * marks it, never recognised by its name.
 */
export default function CloudProfileHeading({
  profile,
  environment,
  environments,
  note,
  disabled,
  onPick,
}: {
  profile: string;
  environment: string;
  environments: EnvironmentView[] | null;
  /** Said after the name, such as why the list on show is not a fresh one. */
  note: string;
  disabled: boolean;
  onPick(environment: string): void;
}) {
  const [open, setOpen] = useState(false);
  const choice = (environments?.length ?? 0) > 1;
  const isDefault = (name: string) => environments?.some((e) => e.name === name && e.default) ?? false;

  return (
    <div className="flex min-w-0 flex-1 flex-col">
      <div className="flex min-w-0 items-center gap-1.5">
        <CloudMark />
        <span className="min-w-0 truncate">
          <b className="font-semibold text-zinc-700 dark:text-zinc-200">{profile}</b> cloud profile
          {!choice && ` · ${environment}`}
          {note}
        </span>
        {!choice && isDefault(environment) && <DefaultPill />}
        {choice && (
          <button
            type="button"
            aria-label="Environment"
            aria-expanded={open}
            disabled={disabled}
            // The heading sits inside the folder menu, which closes on clicks it
            // does not own and reads keys for its search; these are this control's.
            onMouseDown={(e) => e.stopPropagation()}
            onKeyDown={(e) => e.stopPropagation()}
            onClick={() => setOpen((v) => !v)}
            className="ml-auto flex min-w-0 items-center gap-1.5 rounded-md border border-transparent px-2 py-0.5 text-sm text-zinc-600 transition-colors hover:border-black/10 hover:text-zinc-900 disabled:opacity-50 dark:text-zinc-300 dark:hover:border-white/15 dark:hover:text-zinc-100"
          >
            <Layers size={14} className="shrink-0 text-zinc-400" />
            <span className="min-w-0 max-w-[10rem] truncate">{environment}</span>
            {isDefault(environment) && <DefaultPill />}
          </button>
        )}
      </div>
      {choice && open && (
        <ul role="listbox" aria-label="Environments" className="-mx-3 mt-1 border-y border-black/5 dark:border-white/5">
          {(environments ?? []).map((e) =>
            e.name === environment ? (
              <li
                key={e.name}
                role="option"
                aria-selected
                className="flex items-center gap-2 bg-sky-500/10 px-3 py-2 text-sm font-medium text-sky-700 dark:text-sky-300"
              >
                <span className="flex-1 truncate">{e.name}</span>
                {e.default && <DefaultPill />}
              </li>
            ) : (
              <li key={e.name} role="option" aria-selected={false}>
                <button
                  type="button"
                  onMouseDown={(ev) => ev.stopPropagation()}
                  onClick={() => {
                    setOpen(false);
                    onPick(e.name);
                  }}
                  className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-zinc-700 transition-colors hover:bg-black/[0.04] dark:text-zinc-200 dark:hover:bg-white/[0.06]"
                >
                  <span className="flex-1 truncate">{e.name}</span>
                  {e.default && <DefaultPill />}
                </button>
              </li>
            ),
          )}
        </ul>
      )}
    </div>
  );
}
