import { LIST_METHODS, MAP_METHODS, STRING_METHODS, type CelEntry } from "../cel/catalog";
import type { MemberProvider } from "../cel/complete";
import { fieldsAtPath, shapeAtPath } from "./shape";
import type { Field, Scope, ValueShape } from "./types";

/**
 * Turning a Scope into what the completion menu consumes.
 *
 * The only file here that knows `cel/` exists, which is what keeps the model itself
 * testable as plain data and leaves `cel/complete.ts` unchanged: it already takes a
 * MemberProvider, and this is one.
 */

/** How a shape reads in the menu's type column. */
export function describe(shape: ValueShape): string {
  switch (shape.kind) {
    case "object":
      return "map(string, dyn)";
    case "list":
      return `list(${describe(shape.of)})`;
    case "unknown":
      return "dyn";
    default:
      return shape.kind;
  }
}

/** The reason line under a suggestion — never a value, only where the belief is from. */
function summary(name: string, f: Field): string {
  const base = f.note ?? `${name} in scope`;
  return f.certain ? base : `${base} — not on every path`;
}

function toEntry(name: string, f: Field): CelEntry {
  return {
    name,
    kind: "variable",
    signature: describe(f.shape),
    summary: summary(name, f),
    example: name,
  };
}

function entries(fields: Record<string, Field>): CelEntry[] {
  return Object.entries(fields)
    .map(([name, f]) => toEntry(name, f))
    .sort((a, b) => a.name.localeCompare(b.name));
}

/** The receiver methods a resolved value offers, so `body.items.` still completes. */
function methodsFor(shape: ValueShape): CelEntry[] {
  if (shape.kind === "list") return LIST_METHODS;
  if (shape.kind === "string") return STRING_METHODS;
  return [];
}

/**
 * The root names in scope, replacing the catalogue's fixed six.
 *
 * Which is what stops a source payload field from offering `body`: the roots are a
 * property of where the caret is, not a constant.
 */
export function rootsFor(scope: Scope): CelEntry[] {
  return entries(scope.roots);
}

/**
 * Member completion for a dotted path.
 *
 * Returns undefined — not an empty list — when the path cannot be resolved, because
 * `completionsAt` distinguishes them: undefined means "no opinion", which lets the
 * caller fall through, while an empty array asserts there is nothing there.
 */
export function membersFor(scope: Scope): MemberProvider {
  return (path) => {
    const [head, ...rest] = path;
    const root = scope.roots[head];
    if (!root) return undefined;

    const fields = fieldsAtPath(root.shape, rest);
    if (fields) {
      const own = entries(fields);
      // A map's own methods come after its keys: the keys are what was asked for.
      return rest.length === 0 && head === "vars" ? own : [...own, ...MAP_METHODS];
    }

    // Not an object at that path — but it may still be a list or a string, which
    // have methods worth offering even though they have no members. Resolved at the
    // full path, not just the root: `body.lines.` is exactly as much a list as
    // `items.` is, and answering only for the root offered nothing after a dot.
    const at = shapeAtPath(root.shape, rest);
    const methods = at ? methodsFor(at) : [];
    return methods.length > 0 ? methods : undefined;
  };
}
