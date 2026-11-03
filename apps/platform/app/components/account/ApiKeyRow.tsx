"use client";

import { KeyRound, Trash2 } from "lucide-react";
import type { ApiKey } from "@/app/model/apikeys";
import RoleBadges from "@/app/components/admin/users/RoleBadges";

/**
 * One row in the API-keys list: the key's label, the roles it carries and its
 * non-secret prefix…last4 fragment, with its expiry/last-used detail and a delete (revoke) action. The
 * secret token is never shown here — it only ever appears once, at creation. The
 * parent performs the actual delete (and the refresh).
 */
export default function ApiKeyRow({
  apiKey,
  now,
  busy,
  onDelete,
}: {
  apiKey: ApiKey;
  /** Reference time (ms) sampled by the parent at load; 0 before the first load. */
  now: number;
  busy: boolean;
  onDelete: (id: string) => void;
}) {
  const expired = now !== 0 && new Date(apiKey.expiresAt).getTime() < now;

  return (
    <div className="flex items-center gap-3 rounded-lg border border-black/10 px-3 py-2 dark:border-white/10">
      <KeyRound size={14} className="shrink-0 text-zinc-400" />
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{apiKey.name}</span>
          {expired && (
            <span className="rounded bg-red-500/10 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-red-500">
              Expired
            </span>
          )}
        </div>
        <span className="truncate font-mono text-xs text-zinc-400">
          {apiKey.prefix}…{apiKey.last4}
        </span>
        <div className="mt-1">
          <RoleBadges roles={apiKey.roles} />
        </div>
      </div>
      <div className="hidden flex-col items-end text-xs text-zinc-400 sm:flex">
        <span>
          {expired ? "Expired" : "Expires"}{" "}
          {new Date(apiKey.expiresAt).toLocaleDateString()}
        </span>
        <span>
          {apiKey.lastUsedAt
            ? `Last used ${new Date(apiKey.lastUsedAt).toLocaleDateString()}`
            : "Never used"}
        </span>
      </div>
      <button
        type="button"
        aria-label={`Delete ${apiKey.name}`}
        onClick={() => onDelete(apiKey.id)}
        disabled={busy}
        className="rounded-md p-1 text-zinc-400 transition-colors hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
      >
        <Trash2 size={14} />
      </button>
    </div>
  );
}
