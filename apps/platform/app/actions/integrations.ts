"use server";

/**
 * Server actions for integration CRUD — the BFF replacement for the
 * `/api/integrations*` route handlers, including the editor's load/save (the
 * editor consumes these through the `orchestratorFileSystem` capability, not
 * directly). Each action authorizes and delegates to the orchestrator client lib;
 * the model layer unwraps the ActionResult.
 */

import type {
  ConfigFile,
  Integration,
  IntegrationInput,
  IntegrationRename,
} from "@/app/model/orchestrator";
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
  // Attribute the create to the acting user.
  return withWrite((session) =>
    client.createIntegration(input, session.user.id),
  );
}

export async function updateIntegration(
  id: string,
  input: IntegrationRename,
): Promise<ActionResult<Integration>> {
  return withWrite((session) =>
    client.updateIntegration(id, input, session.user.id),
  );
}

export async function readConfigFiles(id: string): Promise<ActionResult<ConfigFile[]>> {
  return withRead(() => client.readConfigFiles(id));
}

export async function replaceConfigFiles(
  id: string,
  files: { path: string; content: string }[],
): Promise<ActionResult<Omit<ConfigFile, "content">[]>> {
  return withWrite((session) => client.replaceConfigFiles(id, files, session.user.id));
}

export async function setIntegrationIcon(
  id: string,
  icon: string,
): Promise<ActionResult<Integration>> {
  return withWrite((session) =>
    client.setIntegrationIcon(id, icon, session.user.id),
  );
}

export async function deleteIntegration(
  id: string,
): Promise<ActionResult<void>> {
  return withWrite(() => client.deleteIntegration(id));
}
