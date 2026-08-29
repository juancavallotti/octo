"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Check, KeyRound, Trash2, X } from "lucide-react";
import type { ClusterSecret } from "@/app/model/secrets";
import type { SecretUse } from "./secretUsage";

const INPUT =
  "rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * One row in the cluster-secrets list: the name, whether anything is using it, and
 * when it was last set, with an inline write-only overwrite editor and a delete
 * action. The value is never shown — overwriting replaces it blind. The row owns
 * its own overwrite open/value state; the parent performs the actual set/delete
 * (and the refresh).
 *
 * "In use" is the answer to the question asked of a secrets list more than any
 * other — whether this one still matters, and what breaks if it is overwritten or
 * deleted. The badge carries the count and opens the detail, because the count is
 * what is scanned and the deployments behind it are what is acted on.
 */
export default function SecretRow({
  secret,
  busy,
  usage,
  onSet,
  onDelete,
}: {
  secret: ClusterSecret;
  busy: boolean;
  /**
   * The deployments binding this secret. Undefined means not known — the
   * deployments could not be listed — and renders no badge at all, since an
   * absent badge otherwise reads as "nothing uses this".
   */
  usage?: SecretUse[];
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
        {usage && <UsageBadge name={secret.name} usage={usage} />}
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

/**
 * The "in use" badge and the popover behind it. Rendered only when usage is known;
 * a secret nothing references gets the muted "Unused" face, which is a fact worth
 * showing on a page whose other job is deciding what can go.
 *
 * Closes on outside click or Escape, mirroring the other popovers in the app.
 */
function UsageBadge({ name, usage }: { name: string; usage: SecretUse[] }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  if (usage.length === 0) {
    return (
      <span
        className="shrink-0 rounded-full bg-black/[0.05] px-2 py-0.5 text-[10px] font-medium text-zinc-400 dark:bg-white/[0.08]"
        title={`No deployment references ${name}`}
      >
        Unused
      </span>
    );
  }

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        title={`${usage.length} deployment${usage.length === 1 ? "" : "s"} reference ${name}`}
        className="rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] font-medium text-emerald-600 transition-colors hover:bg-emerald-500/25 dark:text-emerald-400"
      >
        In use · {usage.length}
      </button>

      {open && (
        <div className="absolute right-0 top-full z-20 mt-1 w-72 rounded-lg border border-black/10 bg-white p-2 shadow-lg dark:border-white/10 dark:bg-zinc-900">
          <p className="px-1 pb-1 text-[10px] font-semibold uppercase tracking-wide text-zinc-400">
            Referenced by
          </p>
          <ul className="flex flex-col gap-0.5">
            {usage.map((use) => (
              <li key={use.deploymentId}>
                <Link
                  href={`/platform/integrations/i/${encodeURIComponent(use.integrationId)}`}
                  className="block rounded-md px-1 py-1 transition-colors hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
                >
                  <span className="flex items-center gap-1.5">
                    <span className="min-w-0 flex-1 truncate text-xs font-medium">
                      {use.integrationName}
                    </span>
                    {use.tag && (
                      <span className="shrink-0 text-[10px] text-zinc-400">
                        {use.tag}
                      </span>
                    )}
                  </span>
                  <span className="block truncate font-mono text-[10px] text-zinc-500 dark:text-zinc-400">
                    {use.vars.join(", ")}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
