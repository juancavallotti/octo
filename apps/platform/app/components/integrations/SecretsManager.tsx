"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Plus, Search } from "lucide-react";
import { filterRanked } from "@octo/util";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  deleteSecret,
  listSecrets,
  setSecret,
  type ClusterSecret,
} from "@/app/model/secrets";
import { listAllDeployments } from "@/app/model/orchestrator";
import SecretRow from "./SecretRow";
import { indexSecretUsage, type SecretUse } from "./secretUsage";

/**
 * Cluster-wide secrets management. Secrets are a shared pool of named values that
 * deployments reference as environment variables. Values are write-only: they can
 * be set or overwritten but never read back, so this view only ever shows a name
 * and when it was last set. The actual value lives in a Kubernetes Secret; the
 * orchestrator never returns it.
 *
 * Owns its own load/refresh/error state, mirroring IntegrationsManager's `run()`
 * pattern. A delete the orchestrator refuses (the secret is still referenced by a
 * deployment) offers a force override.
 *
 * Which secrets are in use is worked out here rather than asked of the
 * orchestrator: a deployment's env bindings come back on the deployment, so the
 * page already has the answer as soon as it has listed them. That listing is
 * allowed to fail — the secrets themselves are the page, and a cluster it cannot
 * reach must not empty it — and when it does, no row claims to know.
 */

const INPUT =
  "rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

// A secret name must be a valid env var name and Kubernetes Secret key at once:
// an uppercase identifier. Mirrors the orchestrator's server-side check.
const NAME_RE = /^[A-Z_][A-Z0-9_]*$/;

export default function SecretsManager() {
  const confirm = useConfirm();
  const [secrets, setSecrets] = useState<ClusterSecret[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [query, setQuery] = useState("");
  /** secret name → the deployments binding it; null until (or unless) known. */
  const [usage, setUsage] = useState<Map<string, SecretUse[]> | null>(null);

  const refresh = useCallback(
    () => listSecrets().then(setSecrets, (e) => setError((e as Error).message)),
    [],
  );

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Usage is a second, optional read: a failure here leaves the badges off rather
  // than reporting an error over a page that is otherwise working.
  useEffect(() => {
    let live = true;
    listAllDeployments().then(
      (ds) => live && setUsage(indexSecretUsage(ds)),
      () => {},
    );
    return () => {
      live = false;
    };
  }, []);

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

  // Ranked, not filtered: a dropped or mistyped letter still finds the secret
  // instead of emptying a list whose names are long and nearly alike.
  const shown = useMemo(
    () => filterRanked(secrets, query, (s) => s.name),
    [secrets, query],
  );

  const nameValid = NAME_RE.test(name);
  const nameTaken = secrets.some((s) => s.name === name);
  const canAdd = !busy && nameValid && !nameTaken && value.length > 0;

  const add = () => {
    if (!canAdd) return;
    run(async () => {
      await setSecret(name, value);
      setName("");
      setValue("");
    });
  };

  const setValueFor = (target: string, next: string) =>
    run(() => setSecret(target, next));

  const remove = async (target: string) => {
    const ok = await confirm({
      title: `Delete secret "${target}"?`,
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    run(async () => {
      try {
        await deleteSecret(target);
      } catch (e) {
        const msg = (e as Error).message;
        if (/in use/i.test(msg)) {
          const force = await confirm({
            title: "Secret is in use",
            body: `${msg}. Force delete anyway? Deployments referencing it will fail on their next restart.`,
            confirmLabel: "Force delete",
            danger: true,
          });
          if (force) {
            await deleteSecret(target, true);
            return;
          }
        }
        throw e;
      }
    });
  };

  return (
    <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
      <div className="mx-auto w-full max-w-2xl">
        <h2 className="text-sm font-semibold">Cluster secrets</h2>
        <p className="mt-1 text-xs text-zinc-400">
          A shared pool of named values. Reference them from a deployment&rsquo;s
          environment variables. Values are write-only — they can be overwritten but
          never shown again.
        </p>

        {error && <p className="mt-3 text-sm text-red-500">{error}</p>}

        {/* Add a secret */}
        <div className="mt-4 flex flex-col gap-2 rounded-lg border border-black/10 p-3 dark:border-white/10">
          <div className="flex flex-wrap items-start gap-2">
            <div className="flex min-w-[10rem] flex-1 flex-col gap-1">
              <input
                value={name}
                disabled={busy}
                placeholder="SECRET_NAME"
                onChange={(e) => setName(e.target.value.toUpperCase())}
                onKeyDown={(e) => e.key === "Enter" && add()}
                className={`${INPUT} w-full font-mono`}
              />
              {name.length > 0 && !nameValid && (
                <span className="text-xs text-amber-500">
                  Use UPPER_SNAKE_CASE (letters, digits, underscore).
                </span>
              )}
              {nameValid && nameTaken && (
                <span className="text-xs text-amber-500">
                  A secret named {name} already exists — overwrite it from the list.
                </span>
              )}
            </div>
            <input
              type="password"
              value={value}
              disabled={busy}
              placeholder="value"
              autoComplete="new-password"
              onChange={(e) => setValue(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && add()}
              className={`${INPUT} min-w-[10rem] flex-1`}
            />
            <button
              type="button"
              onClick={add}
              disabled={!canAdd}
              className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
            >
              <Plus size={14} />
              Add
            </button>
          </div>
        </div>

        {/* Find a secret */}
        {secrets.length > 0 && (
          <div className="mt-4 flex items-center gap-2 rounded-md border border-black/10 px-2 dark:border-white/15">
            <Search size={14} className="shrink-0 text-zinc-400" />
            <input
              value={query}
              placeholder="Search secrets"
              aria-label="Search secrets"
              onChange={(e) => setQuery(e.target.value)}
              className="w-full bg-transparent py-1 text-sm outline-none"
            />
          </div>
        )}

        {/* Existing secrets */}
        <div className="mt-2 flex flex-col gap-1.5">
          {secrets.length === 0 ? (
            <p className="px-1 py-6 text-center text-sm text-zinc-400">
              No secrets yet.
            </p>
          ) : shown.length === 0 ? (
            <p className="px-1 py-6 text-center text-sm text-zinc-400">
              No secret matches “{query}”.
            </p>
          ) : (
            shown.map((s) => (
              <SecretRow
                key={s.name}
                secret={s}
                busy={busy}
                usage={usage ? (usage.get(s.name) ?? []) : undefined}
                onSet={setValueFor}
                onDelete={remove}
              />
            ))
          )}
        </div>
      </div>
    </div>
  );
}
