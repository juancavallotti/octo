"use client";

import { Tag } from "lucide-react";
import type { Runtimes, Snapshot } from "@/app/model/orchestrator";
import VersionMenu from "./VersionMenu";
import RuntimeMenu from "./RuntimeMenu";
import { INPUT } from "./inputStyles";
import Field from "./Field";

/**
 * The rollout dialog's version section: the tag to roll onto (or a new one to
 * cut from the working copy) and, beside it, the runtime the deployment runs.
 */
export default function RolloutVersionField({
  versionMode,
  snapshots,
  deployedTags,
  targetTag,
  onTargetTag,
  creatingNewTag,
  newTag,
  onNewTag,
  runtimes,
  runtime,
  onRuntime,
  busy,
}: {
  versionMode: "pick" | "new-tag";
  snapshots: Snapshot[];
  deployedTags: ReadonlySet<string>;
  /** The picked tag; null is Current, which cuts a new tag. */
  targetTag: string | null;
  onTargetTag: (tag: string | null) => void;
  creatingNewTag: boolean;
  newTag: string;
  onNewTag: (tag: string) => void;
  runtimes: Runtimes | null;
  runtime: string;
  onRuntime: (runtime: string) => void;
  busy: boolean;
}) {
  const runtimeMenu = (
    <RuntimeMenu
      runtimes={runtimes}
      value={runtime}
      onChange={onRuntime}
      disabled={busy}
    />
  );
  const tagInput = (
    <input
      value={newTag}
      disabled={busy}
      placeholder="e.g. v1.0.0"
      onChange={(e) => onNewTag(e.target.value)}
      className={`${INPUT} w-full`}
    />
  );

  if (versionMode === "new-tag") {
    return (
      <Field
        label="New version"
        hint="Ships the on-screen definition: tags the working copy, then rolls out."
      >
        <div className="flex items-center gap-2">
          <Tag size={14} className="shrink-0 text-zinc-400" />
          {tagInput}
          {runtimeMenu}
        </div>
      </Field>
    );
  }
  return (
    <Field
      label="Version"
      hint={
        creatingNewTag
          ? "Current tags the working copy first, then rolls to that frozen version."
          : "Rolls this deployment over to the selected tag's frozen definition."
      }
    >
      <div className="flex flex-wrap items-center gap-2">
        <VersionMenu
          snapshots={snapshots}
          deployedTags={deployedTags}
          value={targetTag}
          onChange={onTargetTag}
        />
        {runtimeMenu}
      </div>
      {creatingNewTag && (
        <div className="mt-2 flex items-center gap-2">
          <Tag size={14} className="shrink-0 text-zinc-400" />
          {tagInput}
        </div>
      )}
    </Field>
  );
}
