"use server";

/**
 * Server actions for environments. Listing is open to anyone signed in, since
 * choosing an environment is how the rest of the platform is scoped; creating,
 * changing and deleting one provisions or removes a namespace, and is the
 * installation's to decide. The orchestrator checks the same split on its side.
 */

import type {
  Environment,
  EnvironmentInput,
  EnvironmentPatch,
} from "@/app/model/environments";
import { withAdmin, withRead } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";

export async function listEnvironments(): Promise<ActionResult<Environment[]>> {
  return withRead(() => client.listEnvironments());
}

export async function createEnvironment(
  input: EnvironmentInput,
): Promise<ActionResult<Environment>> {
  return withAdmin(() => client.createEnvironment(input));
}

export async function updateEnvironment(
  name: string,
  patch: EnvironmentPatch,
): Promise<ActionResult<Environment>> {
  return withAdmin(() => client.updateEnvironment(name, patch));
}

export async function deleteEnvironment(name: string): Promise<ActionResult<void>> {
  return withAdmin(() => client.deleteEnvironment(name));
}
