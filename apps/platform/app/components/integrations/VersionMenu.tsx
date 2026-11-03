"use client";

import { GitBranch, Tag } from "lucide-react";
import type { Snapshot } from "@/app/model/orchestrator";
import MenuButton, { MenuDivider, MenuItem } from "./MenuButton";

/**
 * A GitHub-style version picker: the working copy plus every tag, with a check
 * on the selected one and a dot on each — green for the tags in `deployedTags`,
 * gray otherwise.
 */
export default function VersionMenu({
  snapshots,
  deployedTags,
  value,
  onChange,
  align = "end",
}: {
  snapshots: Snapshot[];
  /** The tags marked with a green dot: the ones the caller calls deployed. */
  deployedTags: ReadonlySet<string>;
  /** The active tag, or null for the working copy. */
  value: string | null;
  onChange: (tag: string | null) => void;
  align?: "start" | "end";
}) {
  return (
    <MenuButton
      icon={<GitBranch size={14} className="text-zinc-400" />}
      label={value ?? "Working copy"}
      title="Select version"
      align={align}
    >
      {(close) => (
        <>
          <MenuItem
            checked={value === null}
            onSelect={() => {
              onChange(null);
              close();
            }}
            icon={<GitBranch size={12} className="shrink-0 text-zinc-400" />}
            trailing={<Dot deployed={false} />}
          >
            Working copy
          </MenuItem>
          {snapshots.length > 0 && <MenuDivider />}
          {snapshots.map((s) => (
            <MenuItem
              key={s.id}
              checked={value === s.tag}
              onSelect={() => {
                onChange(s.tag);
                close();
              }}
              icon={<Tag size={12} className="shrink-0 text-zinc-400" />}
              trailing={<Dot deployed={deployedTags.has(s.tag)} />}
            >
              {s.tag}
            </MenuItem>
          ))}
        </>
      )}
    </MenuButton>
  );
}

/** A version's mark: green when it is deployed, gray otherwise. */
function Dot({ deployed }: { deployed: boolean }) {
  return (
    <span
      aria-label={deployed ? "deployed" : undefined}
      title={deployed ? "Deployed" : undefined}
      className={`h-2 w-2 shrink-0 rounded-full ${
        deployed ? "bg-emerald-500" : "bg-zinc-300 dark:bg-zinc-600"
      }`}
    />
  );
}
