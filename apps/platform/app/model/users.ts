/**
 * Browser-side client for user administration. Backed by the server actions in
 * `app/actions/users.ts`, which reach iam; these wrappers unwrap the
 * ActionResult so callers keep a value-or-throw contract.
 *
 * The types are restated here rather than imported from the client that shapes
 * the wire, for the reason the other models give: that client is server-only,
 * and nothing running in a browser should have to import it to name what comes
 * back.
 */

import * as actions from "@/app/actions/users";
import { unwrap } from "./bff";

/** A user of this platform. */
export interface PlatformUser {
  id: string;
  email: string;
  name: string;
  /** The roles they hold. Always present, empty rather than absent. */
  roles: string[];
  createdAt: string;
  lastLoginAt: string;
}

/** One grantable role, with the sentence iam uses to describe it. */
export interface RoleOption {
  role: string;
  description: string;
}

/** The profile fields an administrator may set. */
export interface UserInput {
  email: string;
  name: string;
}

export async function listUsers(): Promise<PlatformUser[]> {
  return unwrap(await actions.listUsers());
}

export async function listRoles(): Promise<RoleOption[]> {
  return unwrap(await actions.listRoles());
}

export async function createUser(subject: string, input: UserInput): Promise<PlatformUser> {
  return unwrap(await actions.createUser(subject, input));
}

export async function updateUser(id: string, input: UserInput): Promise<PlatformUser> {
  return unwrap(await actions.updateUser(id, input));
}

export async function deleteUser(id: string): Promise<void> {
  return unwrap(await actions.deleteUser(id));
}

export async function grantRole(id: string, role: string): Promise<PlatformUser> {
  return unwrap(await actions.grantRole(id, role));
}

export async function revokeRole(id: string, role: string): Promise<PlatformUser> {
  return unwrap(await actions.revokeRole(id, role));
}
