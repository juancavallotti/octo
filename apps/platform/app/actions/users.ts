"use server";

/**
 * Server actions for user administration — the admin section's people, as
 * opposed to its settings.
 *
 * Every one of them is behind `withAdmin`, and iam checks the caller's role
 * again on its own side. That is not redundancy for its own sake: this boundary
 * decides what the editor may ask for, and iam decides what it will do, and the
 * second check is the one that still holds if somebody reaches iam another way.
 */

import { withAdmin } from "./_auth";
import * as iam from "./client/iamAdmin";
import type {
  PlatformUser,
  RoleOption,
  UserInput,
  UserPage,
  UserQuery,
} from "./client/iam";
import type { ActionResult } from "@octo/http";

export async function listUsers(query: UserQuery): Promise<ActionResult<UserPage>> {
  return withAdmin(() => iam.listUsers(query));
}

export async function listRoles(): Promise<ActionResult<RoleOption[]>> {
  return withAdmin(() => iam.listRoles());
}

export async function createUser(input: UserInput): Promise<ActionResult<PlatformUser>> {
  return withAdmin(() => iam.createUser(input));
}

export async function updateUser(
  id: string,
  input: UserInput,
): Promise<ActionResult<PlatformUser>> {
  return withAdmin(() => iam.updateUser(id, input));
}

export async function deleteUser(id: string): Promise<ActionResult<void>> {
  return withAdmin(() => iam.deleteUser(id));
}

export async function grantRole(
  id: string,
  role: string,
): Promise<ActionResult<PlatformUser>> {
  return withAdmin(() => iam.grantRole(id, role));
}

export async function revokeRole(
  id: string,
  role: string,
): Promise<ActionResult<PlatformUser>> {
  return withAdmin(() => iam.revokeRole(id, role));
}
