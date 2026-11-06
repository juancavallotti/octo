import type {
  Environment,
  EnvironmentInput,
  EnvironmentPatch,
} from "@/app/model/environments";
import type { ActionResult } from "@octo/http";
import { call, enc } from "./http";

// --- Environments -----------------------------------------------------------

export function listEnvironments(): Promise<ActionResult<Environment[]>> {
  return call<Environment[]>("GET", "/environments");
}

export function createEnvironment(
  input: EnvironmentInput,
): Promise<ActionResult<Environment>> {
  return call<Environment>("POST", "/environments", input);
}

export function updateEnvironment(
  name: string,
  patch: EnvironmentPatch,
): Promise<ActionResult<Environment>> {
  return call<Environment>("PATCH", `/environments/${enc(name)}`, patch);
}

export function deleteEnvironment(name: string): Promise<ActionResult<void>> {
  return call<void>("DELETE", `/environments/${enc(name)}`);
}
