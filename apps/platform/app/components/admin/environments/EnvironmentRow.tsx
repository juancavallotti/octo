"use client";

import { useState } from "react";
import { Pencil, Trash2 } from "lucide-react";
import type { Environment, EnvironmentPatch } from "@/app/model/environments";
import { INPUT } from "../fields";

/**
 * One environment. The default one is listed like the others so its namespace
 * is visible, but offers neither edit nor delete: it is the release namespace,
 * and nothing about it is stored to change.
 */
export default function EnvironmentRow({
  environment,
  busy,
  onUpdate,
  onDelete,
}: {
  environment: Environment;
  busy: boolean;
  onUpdate: (name: string, patch: EnvironmentPatch) => Promise<boolean>;
  onDelete: (environment: Environment) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [displayName, setDisplayName] = useState(environment.displayName);
  const [description, setDescription] = useState(environment.description);
  const title = environment.displayName || environment.name;

  const save = async () => {
    if (await onUpdate(environment.name, { displayName, description })) setEditing(false);
  };

  return (
    <li
      aria-label={title}
      className="flex flex-col gap-2 rounded-lg border border-black/10 px-3 py-2 dark:border-white/10"
    >
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium">{title}</span>
            {environment.default && (
              <span className="rounded bg-sky-500/10 px-1.5 py-0.5 text-[10px] font-medium uppercase text-sky-600 dark:text-sky-400">
                Default
              </span>
            )}
          </div>
          <p className="mt-0.5 font-mono text-xs text-zinc-500">
            {environment.name} · namespace {environment.namespace}
          </p>
          {!editing && environment.description && (
            <p className="mt-1 text-xs text-zinc-500">{environment.description}</p>
          )}
        </div>
        {!environment.default && !editing && (
          <div className="flex shrink-0 gap-1">
            <button
              type="button"
              aria-label={`Edit ${title}`}
              disabled={busy}
              onClick={() => setEditing(true)}
              className="rounded p-1 text-zinc-500 hover:bg-black/5 disabled:opacity-50 dark:hover:bg-white/10"
            >
              <Pencil size={14} />
            </button>
            <button
              type="button"
              aria-label={`Delete ${title}`}
              disabled={busy}
              onClick={() => onDelete(environment)}
              className="rounded p-1 text-red-500 hover:bg-red-500/10 disabled:opacity-50"
            >
              <Trash2 size={14} />
            </button>
          </div>
        )}
      </div>
      {editing && (
        <div className="flex flex-wrap items-center gap-2">
          <input
            aria-label="Display name"
            value={displayName}
            disabled={busy}
            onChange={(e) => setDisplayName(e.target.value)}
            className={`${INPUT} min-w-[8rem] flex-1`}
          />
          <input
            aria-label="Description"
            value={description}
            disabled={busy}
            onChange={(e) => setDescription(e.target.value)}
            className={`${INPUT} min-w-[8rem] flex-[2]`}
          />
          <button
            type="button"
            disabled={busy}
            onClick={save}
            className="rounded-md bg-sky-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-sky-500 disabled:opacity-50"
          >
            Save
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => setEditing(false)}
            className="rounded-md px-2.5 py-1 text-xs text-zinc-500 hover:bg-black/5 dark:hover:bg-white/10"
          >
            Cancel
          </button>
        </div>
      )}
    </li>
  );
}
