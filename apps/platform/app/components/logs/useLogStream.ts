/**
 * useLogStream owns the logs view's data lifecycle: filters (mirrored to the URL),
 * the chronological entry list, paging older rows in, and live tailing with
 * stick-to-bottom auto-scroll. The component consumes the returned state + handlers
 * and is left with just rendering.
 */

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { useSearchParams } from "next/navigation";
import { useReplaceQuery } from "@/app/components/useReplaceQuery";
import { listLogs, type LogEntry } from "@/app/model/logs";
import type { AppOption, FilterValues } from "./LogsFilters";
import {
  buildFilters,
  byTimeAsc,
  DEBOUNCE_MS,
  PAGE_SIZE,
  readFilters,
  STICK_THRESHOLD,
  TAIL_POLL_MS,
  TAIL_SIZE,
  writeFilters,
} from "./query";

export interface LogStream {
  filters: FilterValues;
  setFilters: (f: FilterValues) => void;
  entries: LogEntry[];
  apps: AppOption[];
  olderCursor: string | null;
  error: string | null;
  loading: boolean;
  loadingMore: boolean;
  tailing: boolean;
  toggleTail: () => void;
  loadMore: () => void;
  scrollRef: React.RefObject<HTMLDivElement | null>;
  onScroll: () => void;
}

export function useLogStream(): LogStream {
  const replaceQuery = useReplaceQuery();
  const searchParams = useSearchParams();

  // Initialize filters from the URL once; thereafter the URL follows the filters.
  const [filters, setFilters] = useState<FilterValues>(() =>
    readFilters(new URLSearchParams(searchParams.toString())),
  );
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [apps, setApps] = useState<AppOption[]>([]);
  const [olderCursor, setOlderCursor] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [tailing, setTailing] = useState(false);

  const scrollRef = useRef<HTMLDivElement>(null);
  const reqSeq = useRef(0); // guards against an older query overwriting a newer one
  const stick = useRef(true); // whether to auto-scroll to the bottom on new rows
  const filtersRef = useRef(filters);

  // Keep the latest filters reachable from the tail/load-more callbacks (which read
  // them without re-subscribing) and mirror them into the URL.
  useEffect(() => {
    filtersRef.current = filters;
    replaceQuery(writeFilters(filters));
  }, [filters, replaceQuery]);

  // Accumulate the distinct apps seen so the dropdown stays populated even after the
  // user scopes to one (the results would otherwise narrow to it).
  const mergeApps = useCallback((items: LogEntry[]) => {
    setApps((prev) => {
      const seen = new Set(prev.map((a) => a.deploymentId));
      const next = [...prev];
      for (const e of items) {
        if (e.deploymentId && !seen.has(e.deploymentId)) {
          seen.add(e.deploymentId);
          next.push({
            deploymentId: e.deploymentId,
            appName: e.appName,
            appVersion: e.appVersion,
          });
        }
      }
      next.sort((a, b) =>
        (a.appName || a.deploymentId).localeCompare(b.appName || b.deploymentId),
      );
      return next;
    });
  }, []);

  // Re-query from the top whenever filters change (debounced). Replaces the list.
  const load = useCallback(
    (f: FilterValues) => {
      const seq = ++reqSeq.current;
      setLoading(true);
      listLogs(buildFilters(f, undefined, PAGE_SIZE))
        .then(
          (page) => {
            if (seq !== reqSeq.current) return;
            stick.current = true; // a fresh query lands us at the newest rows
            setEntries([...page.items].sort(byTimeAsc));
            setOlderCursor(page.nextBefore);
            mergeApps(page.items);
            setError(null);
          },
          (e) => {
            if (seq === reqSeq.current) setError((e as Error).message);
          },
        )
        .finally(() => {
          if (seq === reqSeq.current) setLoading(false);
        });
    },
    [mergeApps],
  );

  useEffect(() => {
    const id = setTimeout(() => load(filters), DEBOUNCE_MS);
    return () => clearTimeout(id);
  }, [filters, load]);

  // Tail loop: poll the newest page and merge in anything new (by id).
  useEffect(() => {
    if (!tailing) return;
    const poll = () =>
      listLogs(buildFilters(filtersRef.current, undefined, TAIL_SIZE)).then(
        (page) => {
          if (page.items.length > 0) {
            setEntries((prev) => {
              const ids = new Set(prev.map((e) => e.id));
              const fresh = page.items.filter((e) => !ids.has(e.id));
              return fresh.length ? [...prev, ...fresh].sort(byTimeAsc) : prev;
            });
            mergeApps(page.items);
          }
          setError(null);
        },
        (e) => setError((e as Error).message),
      );
    poll();
    const id = setInterval(poll, TAIL_POLL_MS);
    return () => clearInterval(id);
  }, [tailing, mergeApps]);

  const loadMore = useCallback(() => {
    if (!olderCursor) return;
    setLoadingMore(true);
    const el = scrollRef.current;
    const prevHeight = el?.scrollHeight ?? 0;
    listLogs(buildFilters(filtersRef.current, olderCursor, PAGE_SIZE))
      .then(
        (page) => {
          setEntries((prev) => {
            const ids = new Set(prev.map((e) => e.id));
            const older = page.items.filter((e) => !ids.has(e.id));
            return [...older, ...prev].sort(byTimeAsc);
          });
          setOlderCursor(page.nextBefore);
          mergeApps(page.items);
          setError(null);
          // Older rows prepend above; restore the offset so the viewport stays put.
          requestAnimationFrame(() => {
            if (el) el.scrollTop += el.scrollHeight - prevHeight;
          });
        },
        (e) => setError((e as Error).message),
      )
      .finally(() => setLoadingMore(false));
  }, [olderCursor, mergeApps]);

  // Track whether the view is stuck to the bottom, so tailing only auto-scrolls
  // when the user hasn't scrolled up to read history.
  const onScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    stick.current =
      el.scrollHeight - el.scrollTop - el.clientHeight < STICK_THRESHOLD;
  }, []);

  // After rows change, follow the bottom when stuck.
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [entries]);

  const toggleTail = useCallback(() => {
    stick.current = true; // starting a tail jumps to and follows the newest rows
    setTailing((t) => !t);
  }, []);

  return {
    filters,
    setFilters,
    entries,
    apps,
    olderCursor,
    error,
    loading,
    loadingMore,
    tailing,
    toggleTail,
    loadMore,
    scrollRef,
    onScroll,
  };
}
