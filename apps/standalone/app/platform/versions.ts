/**
 * The tag a new version would most likely want: the newest one's patch number plus
 * one, the way octopaas picks a tag when a deploy freezes the draft. Empty when there
 * is nothing to go on, and the field is left for the user.
 */
export function nextTag(tags: string[]): string {
  let best: [number, number, number] | null = null;
  for (const tag of tags) {
    const m = /^v?(\d+)\.(\d+)\.(\d+)$/.exec(tag);
    if (!m) continue;
    const v: [number, number, number] = [Number(m[1]), Number(m[2]), Number(m[3])];
    if (!best || v[0] > best[0] || (v[0] === best[0] && (v[1] > best[1] || (v[1] === best[1] && v[2] > best[2])))) {
      best = v;
    }
  }
  if (!best) return tags.length === 0 ? "v0.1.0" : "";
  return `v${best[0]}.${best[1]}.${best[2] + 1}`;
}
