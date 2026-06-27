"use server";

/**
 * Server action for the orchestrator availability probe — the BFF replacement for
 * GET /api/orchestrator. Reports whether integration features are available: true
 * only when ORCHESTRATOR_URL is set and the orchestrator answers its health check.
 * Unauthenticated by design (mirrors the old route), so the layout can probe it to
 * decide whether to show the integration UI.
 */

import * as client from "./_client";

export async function orchestratorAvailable(): Promise<boolean> {
  return client.checkHealth();
}
