"use client";

import { useEffect, useState } from "react";
import { listQueueStats, type QueueDestination } from "@/app/model/queues";

/**
 * The topic subjects something is currently subscribed to.
 *
 * Read off the broker's own monitoring view, which is the only place that knows.
 * A deployment's topics are scoped `octo.<deployment>.t.<subject>`, so a live
 * subscription tells us both which app is listening and what it is listening for
 * — which is exactly what a topic action needs, and exactly what somebody would
 * otherwise have to know by heart.
 *
 * Suggestions rather than the whole choice: a receiver that has not been deployed
 * yet has no subscription, and refusing to let somebody set the watch up first
 * would be worse than letting them type a subject.
 */

/** Topics are scoped with a `t` segment; queues use `q` and are not destinations. */
const TOPIC_SUBJECT = /^octo\.([^.]+)\.t\.(.+)$/;

export interface TopicDestination {
  deploymentId: string;
  subject: string;
  /** How many clients are on it, so a stale one is visible as zero. */
  subscribers: number;
}

export function useTopicDestinations(): {
  destinations: TopicDestination[];
  loaded: boolean;
} {
  const [destinations, setDestinations] = useState<TopicDestination[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let stopped = false;
    const load = async () => {
      try {
        const stats = await listQueueStats();
        if (stopped) return;
        setDestinations(parse(stats.destinations));
      } catch {
        // The monitoring endpoint is optional and this is a convenience: with no
        // suggestions the subject is simply typed, which is the same experience
        // as before it existed.
      } finally {
        if (!stopped) setLoaded(true);
      }
    };
    void load();
    return () => {
      stopped = true;
    };
  }, []);

  return { destinations, loaded };
}

function parse(rows: QueueDestination[]): TopicDestination[] {
  const out: TopicDestination[] = [];
  for (const row of rows) {
    const match = TOPIC_SUBJECT.exec(row.subject);
    if (!match) continue;
    out.push({
      deploymentId: match[1],
      subject: match[2],
      subscribers: row.subscribers?.length ?? 0,
    });
  }
  return out;
}
