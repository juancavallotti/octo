/**
 * How full the platform's two stores are — the storage-report client, the
 * middle layer between the storage server action and `@octo/http`:
 *
 *     serverAction (auth) → this client (getStorageStats()) → requestJson() → fetch
 *
 * The deeper half of what `client/health.ts` deliberately refuses to answer. That
 * one asks the orchestrator whether each dependency answered a round trip; this
 * one asks the observability service what a round trip cannot tell you — memory
 * against the ceiling, the hit rate, what has been evicted, how much of that
 * service's connection pool is in use, how large the KV table has grown. It is
 * served there because that service holds both stores and is the heaviest writer
 * to one of them.
 *
 * Either half may be null. An installation with no Redis is supported (volatile
 * objects fall back to the database), so `redisReason` distinguishes "this
 * installation has none" from "it is down" — the page must not show those the same
 * way.
 */

import { requestJson, type ActionResult } from "@octo/http";
import { observabilityBaseUrl, observabilityUnconfigured } from "./_observability";

/** Redis counters, from INFO and DBSIZE. */
export interface RedisStats {
  version: string;
  uptimeSeconds: number;
  connectedClients: number;
  blockedClients: number;
  usedMemoryBytes: number;
  peakMemoryBytes: number;
  /** 0 means no ceiling is configured — not a usage of 0% or of 100%. */
  maxMemoryBytes: number;
  /** e.g. "allkeys-lru": what the server does on reaching the ceiling. */
  maxMemoryPolicy: string;
  keyCount: number;
  /** Cumulative since the server started; the rate over a tiny sample says nothing. */
  keyspaceHits: number;
  keyspaceMisses: number;
  /** hits / (hits + misses), or 0 when nothing has been looked up. */
  hitRate: number;
  evictedKeys: number;
  expiredKeys: number;
  totalCommands: number;
  opsPerSecond: number;
}

/** The observability service's connection-pool accounting plus two sizes read from Postgres. */
export interface DatabaseStats {
  totalConns: number;
  acquiredConns: number;
  idleConns: number;
  maxConns: number;
  /** How often a caller waited for a connection — the number that turns "telemetry is late" into "the pool is too small". */
  emptyAcquireCount: number;
  databaseBytes: number;
  /** kv_store including its indexes. */
  kvTableBytes: number;
  kvRowCount: number;
}

export interface StorageStats {
  redis: RedisStats | null;
  database: DatabaseStats | null;
  /** Why a half is absent: not configured, or not reachable. */
  redisReason?: string;
  databaseReason?: string;
}

/** Read the report, or an error result when the observability service is unconfigured. */
export function getStorageStats(): Promise<ActionResult<StorageStats>> {
  const base = observabilityBaseUrl();
  if (!base) {
    return Promise.resolve(observabilityUnconfigured("storage report"));
  }
  return requestJson<StorageStats>("GET", `${base}/settings/storage`);
}
