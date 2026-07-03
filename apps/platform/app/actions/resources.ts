"use server";

/**
 * Server actions for integration resources (env files, templates) — the BFF
 * layer over the orchestrator's resource routes. Each action authorizes and
 * delegates to the orchestrator client lib; the model layer unwraps the
 * ActionResult. Get/delete are nested under the integration, so both ids flow
 * through.
 */

import type { Resource } from "@/app/model/orchestrator";
import { withRead, withWrite } from "./_auth";
import * as client from "./_client";
import type { ActionResult } from "./_client";

export async function listResources(
  integrationId: string,
): Promise<ActionResult<Resource[]>> {
  return withRead(() => client.listResources(integrationId));
}

export async function createResource(
  integrationId: string,
  kind: string,
  name: string,
  content: string,
): Promise<ActionResult<Resource>> {
  return withWrite(() =>
    client.createResource(integrationId, kind, name, content),
  );
}

export async function deleteResource(
  integrationId: string,
  id: string,
): Promise<ActionResult<void>> {
  return withWrite(() => client.deleteResource(integrationId, id));
}

export async function updateResource(
  integrationId: string,
  id: string,
  kind: string,
  name: string,
  content: string,
): Promise<ActionResult<Resource>> {
  return withWrite(() =>
    client.updateResource(integrationId, id, kind, name, content),
  );
}

/** Create or replace a resource by its name (e.g. the editor's `.env.dev`). */
export async function upsertResource(
  integrationId: string,
  kind: string,
  name: string,
  content: string,
): Promise<ActionResult<Resource>> {
  return withWrite(() =>
    client.upsertResourceByName(integrationId, kind, name, content),
  );
}
