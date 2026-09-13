"use client";

import { useEffect, useState } from "react";
import type { DeployOptions, EnvBindingInput } from "@/app/model/orchestrator";
import { listSecrets } from "@/app/model/secrets";
import { emptyBinding, type EnvBinding } from "./DeployEnvFields";

/** Convert persisted wire bindings ({value|secret}) into the modal's editable form. */
function bindingsFromInput(
  initial?: Record<string, EnvBindingInput>,
): Record<string, EnvBinding> {
  const out: Record<string, EnvBinding> = {};
  for (const [name, b] of Object.entries(initial ?? {})) {
    out[name] = b.secret
      ? { mode: "secret", value: "", secret: b.secret }
      : { mode: "value", value: b.value ?? "", secret: "" };
  }
  return out;
}

/**
 * The deploy modal's environment-variable bindings: one binding per variable the
 * integration declares (seeded from its default), the cluster secret names for the
 * picker, whether every required variable is filled, and the wire payload.
 *
 * `initial` seeds the bindings from a deployment's existing env, read once at
 * mount — remount to seed it again.
 */
export function useDeployEnv(
  opts: DeployOptions | null,
  initial?: Record<string, EnvBindingInput>,
) {
  const envVars = opts?.envVars ?? [];
  // Vars an .env resource already supplies: a required one here is satisfied, so it
  // neither blocks the deploy nor needs a value sent — but an explicit value or
  // secret below still overrides it.
  const providedByFile = new Set(opts?.envProvidedKeys ?? []);
  const [bindings, setBindings] = useState<Record<string, EnvBinding>>(() =>
    bindingsFromInput(initial),
  );
  const [secretNames, setSecretNames] = useState<string[]>([]);

  // Load the cluster secret names once; empty on failure (secrets are optional, and
  // a missing reference is flagged server-side at deploy time anyway).
  useEffect(() => {
    let active = true;
    listSecrets().then(
      (s) => active && setSecretNames(s.map((x) => x.name)),
      () => active && setSecretNames([]),
    );
    return () => {
      active = false;
    };
  }, []);

  // Bindings are lazy: an untouched variable has no entry and falls back to its
  // declared default, so there is no seed to keep in sync with the options.
  const setBinding = (name: string, patch: Partial<EnvBinding>) =>
    setBindings((prev) => ({
      ...prev,
      [name]: { ...(prev[name] ?? emptyBinding()), ...patch },
    }));

  // Required variables not yet satisfied — no non-empty value (its default counts,
  // since `build` sends it explicitly), no chosen secret, and no .env resource
  // supplying the key. Surfaced so the modal can name what blocks the deploy.
  const missingRequired = envVars
    .filter((ev) => ev.required)
    .filter((ev) => !providedByFile.has(ev.name))
    .filter((ev) => {
      const b = bindings[ev.name] ?? emptyBinding(ev.default);
      return b.mode === "secret" ? b.secret === "" : b.value.trim() === "";
    })
    .map((ev) => ev.name);
  const complete = missingRequired.length === 0;

  // Build the wire payload. Variables the user set are sent as typed. A required
  // variable left untouched is sent with its declared default *explicitly*: the
  // runtime does not apply a default to satisfy a required var, so it must travel
  // as a real value. Optional untouched variables are omitted.
  const build = (): Record<string, EnvBindingInput> => {
    const env: Record<string, EnvBindingInput> = {};
    for (const ev of envVars) {
      const b = bindings[ev.name];
      if (!b) {
        // Force a required var's default only when nothing else supplies it, so an
        // .env resource's value wins when it has one.
        if (ev.required && ev.default && !providedByFile.has(ev.name))
          env[ev.name] = { value: ev.default };
        continue;
      }
      if (b.mode === "secret") {
        if (b.secret) env[ev.name] = { secret: b.secret };
      } else if (b.value !== "") {
        env[ev.name] = { value: b.value };
      }
    }
    return env;
  };

  return {
    envVars,
    bindings,
    secretNames,
    setBinding,
    complete,
    missingRequired,
    providedKeys: providedByFile,
    build,
  };
}
