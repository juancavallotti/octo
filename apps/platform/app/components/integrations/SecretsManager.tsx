"use client";

import { useCallback, useEffect, useState } from "react";
import { Plus } from "lucide-react";
import {
  deleteSecret,
  listSecrets,
  setSecret,
  type ClusterSecret,
} from "@/app/model/secrets";
import SecretRow from "./SecretRow";

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
 */

const INPUT =
  "rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

// A secret name must be a valid env var name and Kubernetes Secret key at once:
// an uppercase identifier. Mirrors the orchestrator's server-side check.
const NAME_RE = /^[A-Z_][A-Z0-9_]*$/;

export default function SecretsManager() {
  const [secrets, setSecrets] = useState<ClusterSecret[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [name, setName] = useState("");
  const [value, setValue] = useState("");

  const refresh = useCallback(
    () => listSecrets().then(setSecrets, (e) => setError((e as Error).message)),
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

  const remove = (target: string) => {
    if (!confirm(`Delete secret "${target}"?`)) return;
    run(async () => {
      try {
        await deleteSecret(target);
      } catch (e) {
        const msg = (e as Error).message;
        if (
          /in use/i.test(msg) &&
          confirm(
            `${msg}.\n\nForce delete anyway? Deployments referencing it will fail on their next restart.`,
          )
        ) {
          await deleteSecret(target, true);
        } else {
          throw e;
        }
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

        {/* Existing secrets */}
        <div className="mt-4 flex flex-col gap-1.5">
          {secrets.length === 0 ? (
            <p className="px-1 py-6 text-center text-sm text-zinc-400">
              No secrets yet.
            </p>
          ) : (
            secrets.map((s) => (
              <SecretRow
                key={s.name}
                secret={s}
                busy={busy}
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
