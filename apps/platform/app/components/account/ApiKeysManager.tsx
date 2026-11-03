"use client";

import { useCallback, useEffect, useState } from "react";
import { Plus } from "lucide-react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import { useRoles } from "@/app/auth/RolesContext";
import { KEY_ROLES, ROLE_LABELS, reaches } from "@/app/auth/roles";
import {
  createApiKey,
  deleteApiKey,
  listApiKeys,
  type ApiKey,
  type CreatedApiKey,
} from "@/app/model/apikeys";
import ApiKeyRow from "./ApiKeyRow";
import NewKeyReveal from "./NewKeyReveal";

/**
 * Per-user API-key management. Keys authenticate automation (CI, the CLI, the
 * public API) as the signed-in user, limited to the roles chosen for the key. Only
 * roles the user reaches are offered, and never admin; iam enforces both. The full
 * token is shown exactly once, right after creation (see NewKeyReveal); the list
 * only ever shows a key's label, roles, non-secret prefix…last4 fragment and expiry.
 *
 * Owns its own load/refresh/error state, mirroring SecretsManager's `run()` pattern.
 */

const INPUT =
  "rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

const DAY_MS = 86_400_000;
const PRESET_DAYS = [7, 30, 90, 365];
/** iam refuses an expiry more than two years out. */
const MAX_DAYS = 730;

/** The local calendar date `days` from today, as a date input's YYYY-MM-DD value. */
function dateIn(days: number): string {
  const d = new Date(Date.now() + days * DAY_MS);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** The start of a YYYY-MM-DD local date, as the RFC3339 instant iam takes. */
function expiryOf(date: string): string {
  return new Date(`${date}T00:00:00`).toISOString();
}

export default function ApiKeysManager() {
  const confirm = useConfirm();
  const { roles: held } = useRoles();
  const offered = KEY_ROLES.filter((r) => reaches(held, r));
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [name, setName] = useState("");
  const [roles, setRoles] = useState<string[]>([]);
  const [expires, setExpires] = useState(() => dateIn(30));
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

  /** Run a mutation, then refresh; surface failures inline. */
  const run = useCallback(
    async (fn: () => Promise<unknown>) => {
      setBusy(true);
      setError(null);
      try {
        await fn();
        await refresh();
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [refresh],
  );

  const canAdd = !busy && name.trim().length > 0 && roles.length > 0 && expires !== "";

  const toggle = (role: string) =>
    setRoles((rs) => (rs.includes(role) ? rs.filter((r) => r !== role) : [...rs, role]));

  const add = () => {
    if (!canAdd) return;
    run(async () => {
      const key = await createApiKey(name.trim(), roles, expiryOf(expires));
      setCreated(key);
      setName("");
      setRoles([]);
    });
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

        {/* Create a key */}
        <div className="mt-4 flex flex-wrap items-center gap-2 rounded-lg border border-black/10 p-3 dark:border-white/10">
          <input
            value={name}
            disabled={busy}
            placeholder="Key name (e.g. laptop, ci)"
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && add()}
            className={`${INPUT} min-w-[12rem] flex-1`}
          />
          <input
            type="date"
            value={expires}
            min={dateIn(1)}
            max={dateIn(MAX_DAYS)}
            disabled={busy}
            onChange={(e) => setExpires(e.target.value)}
            aria-label="Expires on"
            className={INPUT}
          />
          <button
            type="button"
            onClick={add}
            disabled={!canAdd}
            className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
          >
            <Plus size={14} />
            Create
          </button>
          <div className="flex w-full flex-wrap items-center gap-1 text-xs text-zinc-500">
            <span>Expires in</span>
            {PRESET_DAYS.map((d) => (
              <button
                key={d}
                type="button"
                disabled={busy}
                onClick={() => setExpires(dateIn(d))}
                className="rounded px-1.5 py-0.5 hover:bg-black/5 dark:hover:bg-white/10"
              >
                {d} days
              </button>
            ))}
          </div>
          <fieldset className="flex w-full flex-wrap items-center gap-3 text-sm">
            <legend className="sr-only">Roles</legend>
            <span className="text-xs text-zinc-500">Roles</span>
            {offered.length === 0 ? (
              <span className="text-xs text-zinc-400 italic">
                You hold no role a key can carry.
              </span>
            ) : (
              offered.map((r) => (
                <label key={r} className="inline-flex items-center gap-1.5">
                  <input
                    type="checkbox"
                    checked={roles.includes(r)}
                    disabled={busy}
                    onChange={() => toggle(r)}
                  />
                  {ROLE_LABELS[r] ?? r}
                </label>
              ))
            )}
          </fieldset>
        </div>

        {created && (
          <NewKeyReveal created={created} onDismiss={() => setCreated(null)} />
        )}

        {/* Existing keys */}
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
