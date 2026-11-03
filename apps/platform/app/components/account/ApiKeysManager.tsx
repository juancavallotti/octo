"use client";

import { useCallback, useEffect, useState } from "react";
import { Trash2 } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  createApiKey,
  deleteApiKey,
  listApiKeys,
  type ApiKey,
  type CreatedApiKey,
} from "@/app/model/apikeys";
import ApiKeyRow from "./ApiKeyRow";
import NewKeyReveal from "./NewKeyReveal";
import NewKeyForm from "./NewKeyForm";

/**
 * Per-user API-key management. Keys authenticate automation (CI, the CLI, the
 * public API) as the signed-in user, limited to the roles chosen for the key (see
 * NewKeyForm). The full token is shown exactly once, right after creation (see NewKeyReveal); the list
 * only ever shows a key's label, roles, non-secret prefix…last4 fragment and expiry.
 *
 * Owns its own load/refresh/error state, mirroring SecretsManager's `run()` pattern.
 */
export default function ApiKeysManager() {
  const confirm = useConfirm();
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  // Reference time for the "expired" badge, sampled when the list loads (reading
  // the clock during render is impure). 0 until the first load completes.
  const [now, setNow] = useState(0);

  const refresh = useCallback(
    () =>
      listApiKeys().then(
        (ks) => {
          setKeys(ks);
          setNow(Date.now());
        },
        (e) => setError((e as Error).message),
      ),
    [],
  );

  useEffect(() => {
    refresh();
  }, [refresh]);

  /** Run a mutation, then refresh; surface failures inline. Resolves to whether it worked. */
  const run = useCallback(
    async (fn: () => Promise<unknown>): Promise<boolean> => {
      setBusy(true);
      setError(null);
      try {
        await fn();
        await refresh();
        return true;
      } catch (e) {
        setError((e as Error).message);
        return false;
      } finally {
        setBusy(false);
      }
    },
    [refresh],
  );

  /** Create a key, reporting whether it worked so the form knows to clear. */
  const add = (name: string, roles: string[], expiresAt: string) =>
    run(async () => setCreated(await createApiKey(name, roles, expiresAt)));

  const expired = now === 0 ? [] : keys.filter((k) => new Date(k.expiresAt).getTime() < now);

  const removeExpired = async () => {
    const ok = await confirm({
      title: `Delete ${expired.length} expired ${expired.length === 1 ? "key" : "keys"}?`,
      body: "Expired keys no longer authenticate; this only clears them from the list.",
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    run(() => Promise.all(expired.map((k) => deleteApiKey(k.id))));
  };

  const remove = async (id: string) => {
    const target = keys.find((k) => k.id === id);
    const ok = await confirm({
      title: `Delete key "${target?.name ?? id}"?`,
      body: "Any client using this key will stop working immediately.",
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    run(() => deleteApiKey(id));
  };

  return (
    <div className="flex h-full flex-col px-6 py-5">
      <div className="mx-auto w-full max-w-2xl">
        <h1 className="text-xl font-semibold tracking-tight">API keys</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          Bearer tokens for automation such as CI, the CLI and the public API.
          Give each key only the roles it needs. Send one as{" "}
          <code className="font-mono">Authorization: Bearer &lt;token&gt;</code>.
          The full token is shown only once, when you create it.
        </p>

        {error && <p className="mt-3 text-sm text-red-500">{error}</p>}

        <NewKeyForm busy={busy} onCreate={add} />

        {created && (
          <NewKeyReveal created={created} onDismiss={() => setCreated(null)} />
        )}

        {/* Existing keys */}
        {expired.length > 0 && (
          <div className="mt-4 flex justify-end">
            <button
              type="button"
              onClick={removeExpired}
              disabled={busy}
              className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-zinc-500 transition-colors hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
            >
              <Trash2 size={12} />
              Delete expired ({expired.length})
            </button>
          </div>
        )}
        <div className="mt-4 flex flex-col gap-1.5">
          {keys.length === 0 ? (
            <p className="px-1 py-6 text-center text-sm text-zinc-400">
              No API keys yet.
            </p>
          ) : (
            keys.map((k) => (
              <ApiKeyRow
                key={k.id}
                apiKey={k}
                now={now}
                busy={busy}
                onDelete={remove}
              />
            ))
          )}
        </div>
      </div>
    </div>
  );
}
