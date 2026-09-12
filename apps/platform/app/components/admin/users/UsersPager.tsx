"use client";

import { ChevronLeft, ChevronRight } from "lucide-react";
import type { UsersData } from "./useUsers";

/**
 * Moving through the directory a page at a time.
 *
 * It says which page is on screen and not how many there are, because keyset
 * paging never learns that: the server answers "here is a page, and here is
 * where the next one starts". Counting the directory to fill in a total would be
 * a second query answering a question nobody asked.
 */
export default function UsersPager({ directory }: { directory: UsersData }) {
  const { page, hasPrevious, hasNext, previous, next } = directory;

  // One page and no more is not a thing to navigate.
  if (!hasPrevious && !hasNext) return null;

  return (
    <div className="flex items-center justify-end gap-2 text-sm text-zinc-500">
      <span>Page {page + 1}</span>
      <button
        type="button"
        onClick={previous}
        disabled={!hasPrevious}
        aria-label="Previous page"
        className="rounded-md p-1 transition-colors hover:bg-black/[0.06] disabled:opacity-40 disabled:hover:bg-transparent dark:hover:bg-white/10"
      >
        <ChevronLeft size={16} />
      </button>
      <button
        type="button"
        onClick={next}
        disabled={!hasNext}
        aria-label="Next page"
        className="rounded-md p-1 transition-colors hover:bg-black/[0.06] disabled:opacity-40 disabled:hover:bg-transparent dark:hover:bg-white/10"
      >
        <ChevronRight size={16} />
      </button>
    </div>
  );
}
