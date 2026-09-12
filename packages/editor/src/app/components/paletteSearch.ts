import type { PaletteComponent } from "./palette";

/**
 * Ranking for the command palette's filter.
 *
 * The sidebar's own filter is a plain substring test on the label, which is right for
 * a list you are already looking at. A palette you summon and type blindly into is a
 * different problem: you type what you remember of a name, and the first result has to
 * be the one you meant, because Enter takes it without your reading the list.
 *
 * So: subsequence matching (typing "ai-a" finds "AI Agent"), ranked by how tight and
 * how early the match is. Deliberately not a general fuzzy library — the corpus is a
 * few dozen short labels from the capability schema, and a scoring rule small enough
 * to read is one that can be argued with.
 */

export interface RankedComponent extends PaletteComponent {
  /** Index pairs into the label that matched, for highlighting. */
  matched: number[];
}

/**
 * The positions in `text` matching `query` as a subsequence, or null when it does not
 * match at all. Greedy from the left, which is what makes an early match score well.
 */
export function subsequence(text: string, query: string): number[] | null {
  const hay = text.toLowerCase();
  const needle = query.toLowerCase();
  const at: number[] = [];
  let from = 0;
  for (const ch of needle) {
    const hit = hay.indexOf(ch, from);
    if (hit < 0) return null;
    at.push(hit);
    from = hit + 1;
  }
  return at;
}

/** Lower is better. */
function score(label: string, id: string, matched: number[], query: string): number {
  const lower = label.toLowerCase();
  const q = query.toLowerCase();
  // An exact id or label is never not what was meant.
  if (lower === q || id.toLowerCase() === q) return -1000;
  // A run of characters the user actually typed together beats a scatter across
  // the word, and a prefix beats a hit in the middle.
  const span = matched[matched.length - 1] - matched[0];
  const prefix = lower.startsWith(q) ? -100 : 0;
  const contiguous = lower.includes(q) ? -50 : 0;
  return prefix + contiguous + span + matched[0];
}

/**
 * `items` filtered to those matching `query` and ordered best first. An empty query
 * keeps everything in schema order, which is the order the sidebar groups them in —
 * opening the palette should show the palette, not a reshuffle of it.
 */
export function rank(items: PaletteComponent[], query: string): RankedComponent[] {
  const trimmed = query.trim();
  if (trimmed === "") return items.map((item) => ({ ...item, matched: [] }));

  const hits: { item: RankedComponent; score: number }[] = [];
  for (const item of items) {
    // The id is searchable but not highlighted: people who know the runtime type
    // ("ai-agent") should be able to type it even when the label reads differently.
    const matched = subsequence(item.label, trimmed) ?? (subsequence(item.id, trimmed) ? [] : null);
    if (matched === null) continue;
    hits.push({ item: { ...item, matched }, score: score(item.label, item.id, matched.length ? matched : [0], trimmed) });
  }
  return hits.sort((a, b) => a.score - b.score || a.item.label.localeCompare(b.item.label)).map((h) => h.item);
}
