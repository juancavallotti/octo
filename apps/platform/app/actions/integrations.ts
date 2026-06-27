"use server";

/**
 * Server actions for integration CRUD — the BFF replacement for the
 * `/api/integrations*` route handlers, including the editor's load/save (the
 * editor consumes these through the `orchestratorFileSystem` capability, not
 * directly). Each action authorizes and delegates to the orchestrator client lib;
 * the model layer unwraps the ActionResult.
 */

import type { Integration, IntegrationInput } from "@/app/model/orchestrator";
import { withRead, withWrite } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";

export async function listIntegrations(): Promise<ActionResult<Integration[]>> {
  return withRead(() => client.listIntegrations());
}

export async function getIntegration(
  id: string,
): Promise<ActionResult<Integration>> {
  return withRead(() => client.getIntegration(id));
}

export async function createIntegration(
  input: IntegrationInput,
): Promise<ActionResult<Integration>> {
  return withWrite(() => client.createIntegration(input));
}

export async function updateIntegration(
  id: string,
  input: IntegrationInput,
): Promise<ActionResult<Integration>> {
  return withWrite(() => client.updateIntegration(id, input));
}

export async function deleteIntegration(
  id: string,
): Promise<ActionResult<void>> {
  return withWrite(() => client.deleteIntegration(id));
}
