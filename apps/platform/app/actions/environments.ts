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
import { cookies } from "next/headers";
import {
  ENVIRONMENT_COOKIE,
  ENVIRONMENT_COOKIE_MAX_AGE,
  normalizeEnvironment,
  rolesIn,
} from "@/app/lib/environment";
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

/**
 * Work in `name` from now on ("" or "octo" for the default). Refused for an
 * environment that does not exist or where the caller holds nothing, so the
 * cookie never names a place every later call would be refused in.
 */
export async function selectEnvironment(name: string): Promise<ActionResult<void>> {
  return withRead(async (session) => {
    const environment = normalizeEnvironment(name);
    const jar = await cookies();
    if (environment === "") {
      jar.delete(ENVIRONMENT_COOKIE);
      return { ok: true, data: undefined };
    }
    const listed = await client.listEnvironments();
    if (!listed.ok) return listed;
    if (!listed.data.some((e) => e.name === environment)) {
      return { ok: false, error: "unknown environment" };
    }
    if (rolesIn(session.user.roles ?? [], session.user.envRoles, environment).length === 0) {
      return { ok: false, error: "forbidden" };
    }
    jar.set(ENVIRONMENT_COOKIE, environment, {
      path: "/",
      httpOnly: true,
      sameSite: "lax",
      maxAge: ENVIRONMENT_COOKIE_MAX_AGE,
    });
    return { ok: true, data: undefined };
  });
}
