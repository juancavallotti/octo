"use client";

import { Pin } from "lucide-react";
import { RUNTIME_LATEST, type Runtimes } from "@/app/model/orchestratorTypes";
import { INPUT } from "./inputStyles";

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
  return (
    <label className="inline-flex items-center gap-1.5" title="Runtime">
      <Pin
        size={14}
        className={
          value === RUNTIME_LATEST
            ? "text-zinc-400"
            : "text-amber-600 dark:text-amber-400"
        }
      />
      <select
        aria-label="Runtime"
        value={value}
        disabled={disabled || runtimes === null}
        onChange={(e) => onChange(e.target.value)}
        className={INPUT}
      >
        <option value={RUNTIME_LATEST}>
          Latest{runtimes?.current ? ` (${runtimes.current})` : ""}
        </option>
        {options.map((v) => (
          <option key={v} value={v}>
            Pin to {v}
          </option>
        ))}
      </select>
    </label>
  );
}
