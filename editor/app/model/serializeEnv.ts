import type { EnvVar } from "./document";

/**
 * Maps declared environment variables between the editor model and the runtime
 * config's top-level `env:` list (see runtime/types/env.go). Split out of
 * serialize.ts to keep that file focused; the rest of the config mapping lives
 * there.
 */

export interface RuntimeEnv {
  name?: string;
  default?: string;
  required?: boolean;
}

export function envToRuntime(v: EnvVar): RuntimeEnv {
  const out: RuntimeEnv = { name: v.name };
  if (v.default !== undefined && v.default !== "") out.default = v.default;
  if (v.required) out.required = true;
  return out;
}

export function envFromRuntime(v: RuntimeEnv): EnvVar {
  const out: EnvVar = { name: v.name ?? "" };
  if (v.default !== undefined) out.default = v.default;
  if (v.required) out.required = true;
  return out;
}
