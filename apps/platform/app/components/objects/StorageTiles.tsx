"use client";

import {
  Activity,
  Boxes,
  Database,
  Gauge,
  HardDrive,
  Hourglass,
  Plug,
  Table2,
  Target,
  Timer,
  Trash2,
} from "lucide-react";
import { Stat, bytes, duration, num, percent } from "@/app/components/stats/Stat";
import type { StorageStats } from "@/app/model/storage";

/**
 * The tiles for each store. Presentation only — StorageHealth owns the poll and the
 * layout, and these own what each number means and when it is worth alarming about.
 */

const TILES = "grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4";

export function RedisTiles({
  stats,
}: {
  stats: NonNullable<StorageStats["redis"]>;
}) {
  const capped = stats.maxMemoryBytes > 0;
  // Only meaningful against a ceiling. Without one there is nothing to be near,
  // and a bar filled from an invented denominator would be worse than no bar.
  const pressure = capped ? stats.usedMemoryBytes / stats.maxMemoryBytes : 0;

  return (
    <div className={TILES}>
      <Stat
        icon={HardDrive}
        label="Memory used"
        value={bytes(stats.usedMemoryBytes)}
        hint={
          capped
            ? `${percent(pressure)} of ${bytes(stats.maxMemoryBytes)}`
            : "no ceiling configured"
        }
        alert={capped && pressure >= 0.9}
      />
      <Stat
        icon={Gauge}
        label="Peak memory"
        value={bytes(stats.peakMemoryBytes)}
        hint={stats.maxMemoryPolicy ? `policy: ${stats.maxMemoryPolicy}` : undefined}
      />
      <Stat icon={Boxes} label="Keys" value={num(stats.keyCount)} />
      <Stat icon={Plug} label="Clients" value={num(stats.connectedClients)} />
      <Stat
        icon={Target}
        label="Hit rate"
        value={percent(stats.hitRate)}
        hint={`${num(stats.keyspaceHits)} hits / ${num(stats.keyspaceMisses)} misses`}
      />
      <Stat
        icon={Trash2}
        label="Evicted"
        value={num(stats.evictedKeys)}
        hint="dropped to stay under the ceiling"
      />
      <Stat
        icon={Hourglass}
        label="Expired"
        value={num(stats.expiredKeys)}
        hint="reached their own TTL"
      />
      <Stat
        icon={Activity}
        label="Ops/sec"
        value={num(stats.opsPerSecond)}
        hint={`up ${duration(stats.uptimeSeconds)} · v${stats.version}`}
      />
    </div>
  );
}

export function DatabaseTiles({
  stats,
}: {
  stats: NonNullable<StorageStats["database"]>;
}) {
  // A pool with every connection handed out is the shape of a platform that has
  // gone slow without anything being down, which is exactly what this view is for.
  const saturated = stats.maxConns > 0 && stats.acquiredConns >= stats.maxConns;

  return (
    <div className={TILES}>
      <Stat
        icon={Plug}
        label="Connections in use"
        value={`${num(stats.acquiredConns)} / ${num(stats.maxConns)}`}
        hint={`${num(stats.idleConns)} idle`}
        alert={saturated}
      />
      <Stat
        icon={Timer}
        label="Waited for a connection"
        value={num(stats.emptyAcquireCount)}
        hint="times, since startup"
        alert={stats.emptyAcquireCount > 0}
      />
      <Stat
        icon={Database}
        label="Database size"
        value={bytes(stats.databaseBytes)}
      />
      <Stat
        icon={Table2}
        label="Object store"
        value={bytes(stats.kvTableBytes)}
        hint={`${num(stats.kvRowCount)} objects, with indexes`}
      />
    </div>
  );
}
