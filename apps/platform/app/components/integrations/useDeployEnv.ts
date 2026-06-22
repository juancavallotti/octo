"use client";

import { useEffect, useState } from "react";
import type { DeployOptions, EnvBindingInput } from "@/app/model/orchestrator";
import { listSecrets } from "@/app/model/secrets";
import { emptyBinding, type EnvBinding } from "./DeployEnvFields";

/**
 * Manages the deploy modal's environment-variable bindings: one binding per
 * variable the integration declares (seeded from its default), the cluster secret
 * names for the picker, whether every required variable is filled, and building the
 * wire payload. Keeps the modal itself focused on the dialog and its other fields.
 */
export function useDeployEnv(opts: DeployOptions | null) {
  const envVars = opts?.envVars ?? [];
  const [bindings, setBindings] = useState<Record<string, EnvBinding>>({});
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
  // declared default (see DeployEnvFields / the checks below), so there is no seed
  // effect to keep in sync with the options.
  const setBinding = (name: string, patch: Partial<EnvBinding>) =>
    setBindings((prev) => ({
      ...prev,
      [name]: { ...(prev[name] ?? emptyBinding()), ...patch },
    }));

  // Every required variable must be filled — a non-empty value (its default counts),
  // or a chosen secret.
  const complete = envVars.every((ev) => {
    if (!ev.required) return true;
    const b = bindings[ev.name] ?? emptyBinding(ev.default);
    return b.mode === "secret" ? b.secret !== "" : b.value.trim() !== "";
  });

  // Build the wire payload, sending only variables the user actually set. An empty
  // literal means "leave it to the integration's default", so it is omitted.
  const build = (): Record<string, EnvBindingInput> => {
    const env: Record<string, EnvBindingInput> = {};
    for (const ev of envVars) {
      const b = bindings[ev.name];
      if (!b) continue;
      if (b.mode === "secret") {
        if (b.secret) env[ev.name] = { secret: b.secret };
      } else if (b.value !== "") {
        env[ev.name] = { value: b.value };
      }
    }
    return env;
  };

  return { envVars, bindings, secretNames, setBinding, complete, build };
}
