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
  // Vars an .env resource already supplies: a required one here is satisfied, so it
  // neither blocks the deploy nor needs a value sent — but the operator can still
  // override it below with an explicit value or secret.
  const providedByFile = new Set(opts?.envProvidedKeys ?? []);
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

  // Required variables not yet satisfied — a non-empty value (its default counts,
  // since the payload sends that default explicitly, see build) or a chosen secret,
  // or an .env resource that already supplies the key. Surfaced so the modal can
  // name exactly what blocks the deploy.
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
  // variable left untouched is sent with its declared default *explicitly* —
  // unlike an optional one, the runtime does not apply a default to satisfy a
  // required var, so it must travel as a real value (matching the orchestrator's
  // deploy-time check). Optional untouched variables are omitted.
  const build = (): Record<string, EnvBindingInput> => {
    const env: Record<string, EnvBindingInput> = {};
    for (const ev of envVars) {
      const b = bindings[ev.name];
      if (!b) {
        // Force a required var's default only when nothing else supplies it. If an
        // .env resource already provides the key, leave it unset so the file's value
        // wins (an explicit binding below still overrides both).
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
