"use client";

import { useState } from "react";
import { Check, KeyRound, Trash2, X } from "lucide-react";
import type { ClusterSecret } from "@/app/model/secrets";

const INPUT =
  "rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * One row in the cluster-secrets list: the name and when it was last set, with an
 * inline write-only overwrite editor and a delete action. The value is never shown
 * — overwriting replaces it blind. The row owns its own overwrite open/value state;
 * the parent performs the actual set/delete (and the refresh).
 */
export default function SecretRow({
  secret,
  busy,
  onSet,
  onDelete,
}: {
  secret: ClusterSecret;
  busy: boolean;
  onSet: (name: string, value: string) => void;
  onDelete: (name: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");

  const save = () => {
    if (!value) return;
    onSet(secret.name, value);
    setOpen(false);
    setValue("");
  };

  return (
    <div className="flex flex-col gap-2 rounded-lg border border-black/10 px-3 py-2 dark:border-white/10">
      <div className="flex items-center gap-2">
        <KeyRound size={14} className="shrink-0 text-zinc-400" />
        <span className="min-w-0 flex-1 truncate font-mono text-sm">
          {secret.name}
        </span>
        <span className="hidden text-xs text-zinc-400 sm:inline">
          Updated {new Date(secret.lastUpdated).toLocaleString()}
        </span>
        <button
          type="button"
          onClick={() => {
            setValue("");
            setOpen((o) => !o);
          }}
          disabled={busy}
          className="rounded-md px-2 py-1 text-xs text-zinc-600 transition-colors hover:bg-black/[0.06] disabled:opacity-50 dark:text-zinc-300 dark:hover:bg-white/[0.08]"
        >
          Set value
        </button>
        <button
          type="button"
          aria-label={`Delete ${secret.name}`}
          onClick={() => onDelete(secret.name)}
          disabled={busy}
          className="rounded-md p-1 text-zinc-400 transition-colors hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
        >
          <Trash2 size={14} />
        </button>
      </div>

      {open && (
        <div className="flex items-center gap-2">
          <input
            type="password"
            value={value}
            disabled={busy}
            autoFocus
            placeholder="new value"
            autoComplete="new-password"
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && save()}
            className={`${INPUT} flex-1`}
          />
          <button
            type="button"
            aria-label="Save value"
            onClick={save}
            disabled={busy || !value}
            className="rounded-md p-1 text-emerald-600 transition-colors hover:bg-emerald-500/10 disabled:opacity-50"
          >
            <Check size={16} />
          </button>
          <button
            type="button"
            aria-label="Cancel"
            onClick={() => setOpen(false)}
            disabled={busy}
            className="rounded-md p-1 text-zinc-400 transition-colors hover:bg-black/[0.06] disabled:opacity-50 dark:hover:bg-white/[0.08]"
          >
            <X size={16} />
          </button>
        </div>
      )}
    </div>
  );
}
