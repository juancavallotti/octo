/**
 * Grouping fifty metric names into sections somebody can scan.
 *
 * By prefix, because that is how Prometheus namespaces already work and how
 * these three collectors are actually divided: the runtime's own metrics, the Go
 * runtime's, and the process's. Ordered by how often the answer is in them —
 * a deployment's own flows first, the kernel's view of the process last.
 */

export interface Family {
  key: string;
  label: string;
  blurb: string;
  /** Name prefixes that belong to this family. */
  prefixes: string[];
}

export const FAMILIES: Family[] = [
  {
    key: "octo",
    label: "Flows",
    blurb: "What this deployment's own runtime reports.",
    prefixes: ["octo_"],
  },
  {
    key: "process",
    label: "Process",
    blurb: "The kernel's view: CPU, memory, file descriptors, sockets.",
    prefixes: ["process_"],
  },
  {
    key: "go",
    label: "Go runtime",
    blurb: "Heap, garbage collection, goroutines and threads.",
    prefixes: ["go_"],
  },
];

const OTHER: Family = {
  key: "other",
  label: "Other",
  blurb: "Everything the pod exposes that is not one of the families above.",
  prefixes: [],
};

/** Which family a metric belongs to. */
export function familyOf(name: string): Family {
  return FAMILIES.find((f) => f.prefixes.some((p) => name.startsWith(p))) ?? OTHER;
}

/** Group names into families, in the families' own order, dropping the empty
 * ones so a runtime exposing nothing under a heading does not show one. */
export function byFamily<T extends { name: string }>(
  items: T[],
): Array<{ family: Family; items: T[] }> {
  const groups = new Map<string, T[]>();
  for (const item of items) {
    const key = familyOf(item.name).key;
    const existing = groups.get(key);
    if (existing) existing.push(item);
    else groups.set(key, [item]);
  }

  return [...FAMILIES, OTHER]
    .filter((family) => groups.has(family.key))
    .map((family) => ({
      family,
      items: (groups.get(family.key) ?? []).sort(byInterest),
    }));
}

/**
 * Within a family, the metrics worth looking at first.
 *
 * Histogram buckets go last. One of them is a hundred and eight series of a
 * single metric — more than every other series on the page combined — and it is
 * the one whose chart says the least at a glance, so it should not be what a
 * reader meets at the top of a section.
 */
function byInterest<T extends { name: string }>(a: T, b: T): number {
  const rank = (name: string) => (name.endsWith("_bucket") ? 1 : 0);
  const difference = rank(a.name) - rank(b.name);
  return difference !== 0 ? difference : a.name.localeCompare(b.name);
}
