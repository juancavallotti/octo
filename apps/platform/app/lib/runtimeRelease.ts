/**
 * Which octo runtime a deployment made right now would be put on, and whether one
 * that exists is behind it.
 *
 * The answer is the image the orchestrator is configured to deploy, which the
 * chart hands to this app as RUNTIME_IMAGE — the same value, from the same
 * template helper, that it hands the orchestrator. It is read from the environment
 * rather than baked in from release-please because an operator may pin the runtime
 * to a tag this install was not built at, and a badge that called that "behind"
 * would be wrong on every deployment they own.
 *
 * Absent — a dev machine, a standalone editor, an install that never set it —
 * means the comparison cannot be made, and nothing is claimed. Silence is the only
 * honest answer there: every deployment reading "up to date" and every deployment
 * reading "needs upgrade" are both fabrications.
 */

/** Server-only: the runtime image this install deploys, or "" when unset. */
export const CURRENT_RUNTIME_IMAGE = process.env.RUNTIME_IMAGE ?? "";

/**
 * The tag part of a container image reference, or "" when it is untagged or
 * pinned by digest. The colon searched for is the last one, and only past the
 * final slash, so a registry with a port (registry:5000/octo/runtime) is not
 * mistaken for a tag. Mirrors the orchestrator's imageTag, which is what produces
 * the tag a deployment reports.
 */
export function imageTag(image: string): string {
  // A digest-pinned reference names no tag, and its digest is not one: the colon
  // in `@sha256:…` would otherwise be read as the tag separator and put a hash
  // fragment on screen where a version goes.
  if (image.includes("@")) return "";
  const slash = image.lastIndexOf("/");
  const name = slash >= 0 ? image.slice(slash + 1) : image;
  const colon = name.lastIndexOf(":");
  return colon < 0 ? "" : name.slice(colon + 1);
}

/**
 * Whether a deployment's runtime is OLDER than the one this install deploys.
 *
 * Older, not merely different, and the distinction is the whole of this function.
 * A deployment can be ahead — someone rolled it onto a build the chart has not
 * caught up with — and telling them to upgrade to the lower version they are
 * already past is worse than saying nothing.
 *
 * Compared by tag rather than by full reference: a mirrored registry gives the
 * same runtime a different path, and calling that an upgrade would tell everyone
 * running a mirror that all of their deployments are stale. Anything that is not
 * a dotted release number — `dev`, a branch build — is not comparable, and an
 * unknown on either side is not behind. All three are the same answer: say
 * nothing rather than something invented.
 */
export function needsUpgrade(deployed: string | undefined, current: string): boolean {
  if (!deployed || !current || deployed === current) return false;
  const a = release(deployed);
  const b = release(current);
  if (!a || !b) return false;
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    const left = a[i] ?? 0;
    const right = b[i] ?? 0;
    if (left !== right) return left < right;
  }
  return false;
}

/**
 * A dotted release number as its parts, or null for a tag that is not one.
 *
 * A leading `v` is accepted because tags are written both ways; anything after
 * the numbers — a `-rc1`, a `+build` — makes the tag incomparable rather than
 * equal to its own release, since ordering prereleases is a judgement this has no
 * business making from a tag alone.
 */
function release(tag: string): number[] | null {
  const match = /^v?(\d+(?:\.\d+)*)$/.exec(tag.trim());
  return match ? match[1].split(".").map(Number) : null;
}
