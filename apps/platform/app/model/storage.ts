/**
 * Browser-side client for the storage report. Backed by the `getStorageStats`
 * server action; this wrapper unwraps the ActionResult so callers keep a
 * value-or-throw contract. Read-only: a periodic snapshot the object store's
 * Storage Health view polls.
 */

import * as storageActions from "@/app/actions/storage";
import { unwrap } from "./bff";

export type {
  StorageStats,
  RedisStats,
  DatabaseStats,
} from "@/app/actions/client/storage";

import type { StorageStats } from "@/app/actions/client/storage";

export async function getStorageStats(): Promise<StorageStats> {
  return unwrap(await storageActions.getStorageStats());
}
