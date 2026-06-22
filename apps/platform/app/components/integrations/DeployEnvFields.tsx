"use client";

import type { DeployEnvVar } from "@/app/model/orchestrator";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** One declared variable's binding: a literal value or a reference to a secret. */
export type EnvBinding = { mode: "value" | "secret"; value: string; secret: string };

/** An empty binding (literal mode), used to seed a variable with its default. */
export function emptyBinding(value = ""): EnvBinding {
  return { mode: "value", value, secret: "" };
}

/**
 * The deploy modal's environment section: one row per env var the integration
 * declares, each filled with either a literal value or a reference to a cluster
 * secret. Presentational — the parent owns the binding state and submit. A chosen
 * secret that no longer exists is still shown, flagged, mirroring EnvValueField.
 */
export default function DeployEnvFields({
  envVars,
  bindings,
  secretNames,
  busy,
  onChange,
}: {
  envVars: DeployEnvVar[];
  bindings: Record<string, EnvBinding>;
  secretNames: string[];
  busy: boolean;
  onChange: (name: string, patch: Partial<EnvBinding>) => void;
}) {
  return (
    <div className="flex flex-col gap-3">
      {envVars.map((ev) => {
        const b = bindings[ev.name] ?? emptyBinding(ev.default);
        const dangling =
          b.mode === "secret" && b.secret !== "" && !secretNames.includes(b.secret);
        return (
          <div key={ev.name} className="flex flex-col gap-1">
            <div className="flex items-center justify-between gap-2">
              <span className="truncate font-mono text-xs text-zinc-600 dark:text-zinc-300">
                {ev.name}
                {ev.required && <span className="text-red-500"> *</span>}
              </span>
              <div className="flex shrink-0 gap-0.5 rounded bg-black/[0.04] p-0.5 dark:bg-white/[0.06]">
                {(["value", "secret"] as const).map((m) => (
                  <button
                    key={m}
                    type="button"
                    disabled={busy}
                    onClick={() => onChange(ev.name, { mode: m })}
                    className={`rounded px-2 py-0.5 text-xs capitalize transition-colors ${
                      b.mode === m
                        ? "bg-white text-zinc-900 shadow-sm dark:bg-zinc-700 dark:text-white"
                        : "text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
                    }`}
                  >
                    {m}
                  </button>
                ))}
              </div>
            </div>
            {b.mode === "value" ? (
              <input
                type="text"
                value={b.value}
                disabled={busy}
                placeholder={ev.default ? `default: ${ev.default}` : ""}
                onChange={(e) => onChange(ev.name, { value: e.target.value })}
                className={INPUT}
              />
            ) : (
              <select
                value={b.secret}
                disabled={busy}
                onChange={(e) => onChange(ev.name, { secret: e.target.value })}
                className={INPUT}
              >
                <option value="">
                  {secretNames.length === 0
                    ? "— no secrets defined —"
                    : "— select a secret —"}
                </option>
                {dangling && (
                  <option value={b.secret}>{b.secret} (missing)</option>
                )}
                {secretNames.map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            )}
          </div>
        );
      })}
    </div>
  );
}
