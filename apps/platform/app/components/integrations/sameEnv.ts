import type { EnvBindingInput } from "@/app/model/orchestrator";

/** Whether two env binding sets bind the same names the same way. */
export function sameEnv(
  a: Record<string, EnvBindingInput>,
  b: Record<string, EnvBindingInput>,
): boolean {
  const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
  for (const k of keys) {
    if ((a[k]?.value ?? "") !== (b[k]?.value ?? "")) return false;
    if ((a[k]?.secret ?? "") !== (b[k]?.secret ?? "")) return false;
  }
  return true;
}
