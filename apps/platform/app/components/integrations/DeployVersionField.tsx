"use client";

import { Tag } from "lucide-react";
import RuntimeMenu from "./RuntimeMenu";
import { useRuntimes } from "./useRuntimes";
import { INPUT } from "./inputStyles";
import Field from "./Field";

/**
 * The deploy dialog's version section: the tag being deployed (or a new one to
 * cut from the working copy) and, beside it, the runtime the deployment runs.
 */
export default function DeployVersionField({
  currentMode,
  activeVersionLabel,
  newTag,
  onNewTag,
  runtime,
  onRuntime,
  busy,
}: {
  /** Deploying the working copy, which has to be tagged first. */
  currentMode: boolean;
  activeVersionLabel: string;
  newTag: string;
  onNewTag: (tag: string) => void;
  /** RUNTIME_LATEST or a release to pin to. */
  runtime: string;
  onRuntime: (runtime: string) => void;
  busy: boolean;
}) {
  const runtimes = useRuntimes();
  const runtimeMenu = (
    <RuntimeMenu
      runtimes={runtimes}
      value={runtime}
      onChange={onRuntime}
      disabled={busy}
    />
  );
  if (currentMode) {
    return (
      <Field
        label="New version"
        hint="Deploying the working copy tags it first, then ships that frozen version."
      >
        <div className="flex items-center gap-2">
          <Tag size={14} className="shrink-0 text-zinc-400" />
          <input
            value={newTag}
            disabled={busy}
            placeholder="e.g. v1.0.0"
            onChange={(e) => onNewTag(e.target.value)}
            className={`${INPUT} w-full`}
          />
          {runtimeMenu}
        </div>
      </Field>
    );
  }
  return (
    <Field
      label="Version"
      hint="Ships this tag's frozen definition. Pick a different version from the header."
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="inline-flex items-center gap-1.5 rounded-md bg-sky-500/10 px-2 py-1 text-sm font-medium text-sky-600 dark:text-sky-400">
          <Tag size={14} />
          {activeVersionLabel}
        </span>
        {runtimeMenu}
      </div>
    </Field>
  );
}
