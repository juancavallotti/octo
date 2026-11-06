/**
 * Browser-side client for environments. Backed by the server actions in
 * `app/actions/environments.ts`; these wrappers unwrap the ActionResult so
 * callers keep a value-or-throw contract.
 */

import * as actions from "@/app/actions/environments";
import { unwrap } from "./bff";

export { DEFAULT_ENVIRONMENT } from "@/app/lib/environment";

/** An environment and the namespace it runs in. */
export interface Environment {
  name: string;
  namespace: string;
  displayName: string;
  description: string;
  /** True for the default environment, which cannot be changed or deleted. */
  default: boolean;
  /** Absent for the default environment, which nobody created. */
  createdAt?: string;
  createdBy?: string;
}

/** What creating an environment takes. The namespace defaults to `octo-{name}`. */
export interface EnvironmentInput {
  name: string;
  namespace?: string;
  displayName?: string;
  description?: string;
}

/** What may change after creation; an omitted field is left as it is. */
export interface EnvironmentPatch {
  displayName?: string;
  description?: string;
}

export async function listEnvironments(): Promise<Environment[]> {
  return unwrap(await actions.listEnvironments());
}

export async function createEnvironment(input: EnvironmentInput): Promise<Environment> {
  return unwrap(await actions.createEnvironment(input));
}

export async function updateEnvironment(
  name: string,
  patch: EnvironmentPatch,
): Promise<Environment> {
  return unwrap(await actions.updateEnvironment(name, patch));
}

/** Work in `name` from now on ("" or "octo" for the default). */
export async function selectEnvironment(name: string): Promise<void> {
  return unwrap(await actions.selectEnvironment(name));
}

export async function deleteEnvironment(name: string): Promise<void> {
  return unwrap(await actions.deleteEnvironment(name));
}
