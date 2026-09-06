"use server";

/**
 * Server action for the storage report.
 *
 * A read, like the health report next to it: how much memory Redis is using
 * configures nothing and reveals nothing a signed-in user could act on
 * destructively. The reason strings are transport errors from the observability
 * service's own probes and carry no credentials.
 */

import { withRead } from "./_auth";
import * as client from "./_storage";
import type { ActionResult } from "./_client";
import type { StorageStats } from "./_storage";

export async function getStorageStats(): Promise<ActionResult<StorageStats>> {
  return withRead(() => client.getStorageStats());
}
