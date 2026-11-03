"use client";

import { Pin } from "lucide-react";
import { RUNTIME_LATEST, type Runtimes } from "@/app/model/orchestratorTypes";
import MenuButton, { MenuDivider, MenuItem } from "./MenuButton";

/**
 * Which runtime a deployment runs: "latest", which follows this install's
 * runtime as it is upgraded, or a release it stays pinned to until someone
 * changes it. A pin the list does not carry (its release left support) is still
 * offered, so opening the dialog never silently unpins.
 */
export default function RuntimeMenu({
  runtimes,
  value,
  onChange,
  disabled,
}: {
  runtimes: Runtimes | null;
  /** RUNTIME_LATEST or a release. */
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const versions = runtimes?.versions ?? [];
  const options =
    value !== RUNTIME_LATEST && !versions.includes(value)
      ? [value, ...versions]
      : versions;
  const latest = `Latest${runtimes?.current ? ` (${runtimes.current})` : ""}`;
  const pinned = value !== RUNTIME_LATEST;
  return (
    <div className="inline-flex items-center gap-1.5">
      <span className="text-sm text-zinc-500 dark:text-zinc-400">Runtime</span>
      <MenuButton
        icon={
          <Pin
            size={14}
            className={
              pinned ? "text-amber-600 dark:text-amber-400" : "text-zinc-400"
            }
          />
        }
        label={pinned ? `Pinned to ${value}` : latest}
        ariaLabel="Runtime"
        title="Select runtime"
        disabled={disabled || runtimes === null}
        align="start"
      >
        {(close) => (
          <>
            <MenuItem
              checked={!pinned}
              onSelect={() => {
                onChange(RUNTIME_LATEST);
                close();
              }}
            >
              {latest}
            </MenuItem>
            {options.length > 0 && <MenuDivider />}
            {options.map((v) => (
              <MenuItem
                key={v}
                checked={value === v}
                onSelect={() => {
                  onChange(v);
                  close();
                }}
              >
                Pin to {v}
              </MenuItem>
            ))}
          </>
        )}
      </MenuButton>
    </div>
  );
}
