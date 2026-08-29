/**
 * Ranking a typed query against a candidate label.
 *
 * The question a picker actually asks is not "does this contain that" but "how
 * close did they get" — someone typing at speed drops letters, doubles them, and
 * reaches for the wrong key, and a `includes()` filter answers all three with an
 * empty list. This scores a *subsequence*: the query's letters have to appear in
 * order, but not adjacently, and the score falls off with how far apart they had
 * to be spread to find them. A prefix scores 1, a dropped or mistyped letter
 * costs a fraction, and something unrelated lands at 0.
 *
 * It is deliberately not an edit distance. Transpositions ("teh" for "the") are
 * not free here, and that is the trade for a single pass and no matrix: what it
 * buys is that every letter someone did type still has to be in the answer, which
 * is what stops a fuzzy list from returning things that look nothing like the
 * query.
 */

/**
 * How the score treats a query much shorter than what it matched.
 *
 * `unbiased` asks only how tightly the letters were packed, so a three-letter
 * query matches a long name as well as a short one. `favor-closer-length` mixes
 * in how much of the name the query accounted for, which is what a picker wants:
 * typing `ord` should rank `orders` above `orders-reconciliation-worker`.
 */
export type RankBias = "unbiased" | "favor-closer-length";

/**
 * Score `query` against `target`, case-insensitively, from 0 (nothing matched) to
 * 1 (every letter, tightly packed).
 */
export function rankSearchString(
  query: string,
  target: string,
  bias: RankBias = "unbiased",
): number {
  if (typeof query !== "string" || typeof target !== "string") return 0;

  const q = query.toLowerCase();
  const haystack = target.toLowerCase();

  // Where the first matched letter landed, and where the last one did. The span
  // between them is what the score is measured against: the same letters found
  // close together are a better match than the same letters found scattered.
  let firstIndex = -1;
  let lastIndex = -1;
  // The position in the query of the last letter that matched, so letters the
  // target never had are not counted as distance.
  let lastMatchedInQuery = 0;
  let found = 0;

  for (let i = 0; i < q.length; i++) {
    // Resume *past* the previous match, not at it. Resuming at it lets a repeated
    // query letter match the same position twice, which scored "aa" against "cat"
    // as a perfect match.
    const at = haystack.indexOf(q[i], lastIndex >= 0 ? lastIndex + 1 : 0);
    if (at === -1) continue;

    lastIndex = at;
    found++;
    if (firstIndex === -1) {
      firstIndex = at;
      continue;
    }
    lastMatchedInQuery = i;
  }

  // Letters typed after the last one that matched still count against the score —
  // otherwise a query that trails off into nonsense scores like the prefix alone.
  const trailing = q.length - lastMatchedInQuery;
  // `lastIndex` is the last *successful* match rather than the last lookup, so a
  // final letter the target lacks cannot drag the span negative.
  //
  // Every letter the target never had widens the span by one. Without that a
  // wrong key in the middle of a query is free — "pra" and "pora" both scored
  // 0.75 against "asparagus" — and a typo that costs nothing is not a typo the
  // ranking can order around.
  const missed = q.length - found;
  const span = Math.max(lastIndex + trailing - firstIndex, q.length) + missed;
  if (span <= 0) return 0;

  const packed = found / span;
  if (bias === "unbiased") return packed;

  const ratio =
    q.length > target.length ? target.length / q.length : q.length / target.length;
  return 0.3 * ratio + 0.7 * packed;
}

/** Options for {@link filterRanked}. */
export interface FilterRankedOptions {
  /** Lowest score still worth showing. Defaults to 0.4 — a weak but real match. */
  min?: number;
  /** Keep at most this many results. Unlimited by default. */
  limit?: number;
  bias?: RankBias;
}

/**
 * Rank `items` against `query` and return the ones worth showing, best first.
 *
 * An empty query is not a filter — it returns everything, in the order it came,
 * because "I have not typed yet" and "nothing matches" must not look the same.
 */
export function filterRanked<T>(
  items: readonly T[],
  query: string,
  toText: (item: T) => string,
  options: FilterRankedOptions = {},
): T[] {
  const { min = 0.4, limit, bias = "favor-closer-length" } = options;

  const trimmed = query.trim();
  if (!trimmed) return limit === undefined ? [...items] : items.slice(0, limit);

  const scored: { item: T; score: number; at: number }[] = [];
  items.forEach((item, at) => {
    const score = rankSearchString(trimmed, toText(item), bias);
    if (score >= min) scored.push({ item, score, at });
  });

  // Ties keep the caller's order — the list it handed over is usually already
  // sorted by something meaningful (recency, cost), and a fuzzy score should not
  // shuffle equally-good matches out of it.
  scored.sort((a, b) => b.score - a.score || a.at - b.at);

  const ranked = scored.map((entry) => entry.item);
  return limit === undefined ? ranked : ranked.slice(0, limit);
}
