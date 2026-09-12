import type { BlockNode } from "../model/document";
import { getBlockSpec } from "../schema";
import type { BlockSpec, FieldSpec } from "../schema/types";
import { shapeOfExpression } from "./cel";
import { DYN, field, objectOf } from "./shape";
import type { Contribution, Field, Scope, ValueShape } from "./types";

/**
 * What one block is known to add to, or take from, the message.
 *
 * Resolved in two tiers, and the first one is why this file is short. The runtime
 * already names variable-declaring settings by a convention — `resultVar`,
 * `statusVar`, `existsVar`, `claimsVar`, `as`, `name` — so most of the catalogue can
 * be read straight out of `octo schema` with no entry here at all. What remains is
 * the handful of blocks whose effect on the message is not expressible as "this
 * setting names a variable".
 *
 * This is a second hand-mirror of Go, which is the same debt `cel/catalog.ts`
 * documents, and it decays the same way. The mitigation is the rot alarm in
 * scripts/check-scope-contributions.mjs, which reads the real catalogue from
 * `bin/octo schema` in CI: a new block with a variable-naming setting that neither
 * tier accounts for fails that check. It cannot live in a unit test here, because the
 * bundled capabilities.json is an empty fallback and the suite would pass vacuously.
 */

/** Settings whose value is the NAME of a variable the block sets. */
const VAR_NAMING = /(^as$)|(^name$)|(Var$)/;

/**
 * Settings that choose between a variable and the body.
 *
 * The runtime states the contract on the field itself — "when set, store the response
 * here and leave the body; when empty, the response becomes the body" (see
 * `ResultVar` in runtime/connectors/parallel/search.go, `As` in
 * runtime/blocks/builtin/object.go). Left empty, the block replaces the body, and
 * what the editor knew about the old one is now wrong.
 *
 * This is the rule that earns the conventional tier its keep: it covers two dozen
 * blocks that would otherwise each need an entry.
 */
const BODY_OR_VAR = new Set(["resultVar", "as"]);

/** The shapes the runtime documents for its conventionally-named variables. */
const KNOWN_SHAPES: Record<string, ValueShape> = {
  statusVar: { kind: "number" },
  existsVar: { kind: "bool" },
  rawBodyVar: { kind: "string" },
  claimsVar: objectOf({}),
};

/** A setting's effective value: what is set, else what the schema defaults to. */
function value(block: BlockNode, spec: FieldSpec): string | undefined {
  const raw = block.settings[spec.name] ?? spec.default;
  return typeof raw === "string" && raw.trim() !== "" ? raw.trim() : undefined;
}

/**
 * Settings that name a variable scoped to the block's own slot rather than to the
 * flow after it. `foreach`'s `as` is the current element: it exists inside the body
 * and is gone once the loop is over, so the conventional pass must not carry it out.
 * walk.ts seeds it in the slot instead.
 */
const SLOT_SCOPED: Record<string, Set<string>> = { foreach: new Set(["as"]) };

function varsFromConvention(block: BlockNode, spec: BlockSpec): Record<string, Field> {
  const out: Record<string, Field> = {};
  const slotScoped = SLOT_SCOPED[block.type];
  for (const f of spec.fields) {
    if (f.type !== "string" || !VAR_NAMING.test(f.name)) continue;
    if (slotScoped?.has(f.name)) continue;
    const name = value(block, f);
    if (!name) continue;
    out[name] = field(KNOWN_SHAPES[f.name] ?? DYN, "inferred", `set by ${block.type}`);
  }
  return out;
}

/**
 * Blocks whose effect the schema cannot describe.
 *
 * Each returns only what it knows; the conventional pass still runs and is merged in,
 * so an entry here says what is *extra*, not what is instead.
 */
/** A block setting read as a CEL expression, or undefined when it says nothing. */
function fromExpression(
  block: BlockNode,
  setting: string,
  scope: Scope | undefined,
  note: string,
): Field | undefined {
  const raw = block.settings[setting];
  if (typeof raw !== "string") return undefined;
  const shape = shapeOfExpression(raw, scope);
  return shape ? field(shape, "inferred", note) : undefined;
}

const SPECIAL: Record<string, (block: BlockNode, spec: BlockSpec, scope?: Scope) => Contribution> = {
  // The convention already reads `name` as the variable. What it cannot do is say
  // what that variable HOLDS, which `value` often states plainly — a map literal, or
  // a path to something already in scope.
  "set-variable": (block, _spec, scope) => {
    const name = block.settings.name;
    if (typeof name !== "string" || !name) return {};
    const known = fromExpression(block, "value", scope, `set by set-variable "${name}"`);
    return known ? { setVars: { [name]: known } } : {};
  },

  "delete-variable": (block) => {
    const name = block.settings.name;
    return typeof name === "string" && name ? { deleteVars: [name] } : {};
  },

  // The result of the call becomes the body; statusVar (convention) carries the code.
  rest: () => ({ body: "opaque" }),
  "rest-dynamic": () => ({ body: "opaque" }),

  // A set-payload's `value` is the body. When it is a literal it names every key
  // outright, which is the single most introspectable thing in a flow — calling it
  // opaque was leaving the easiest answer on the table.
  "set-payload": (block, _spec, scope) => {
    if (block.settings.rawBody === true) {
      return { body: { kind: "shape", shape: { kind: "string" } } };
    }
    const known = fromExpression(block, "value", scope, "built by set-payload");
    return known ? { body: { kind: "shape", shape: known.shape } } : { body: "opaque" };
  },
  "template-resource": () => ({ body: "opaque" }),

  // An ordered list of {setBody} / {setVar, value} steps — see TransformListEditor.
  // Each step's expression is read the same way set-variable's and set-payload's are.
  "multi-transform": (block, _spec, scope) => {
    const steps = Array.isArray(block.settings.transforms) ? block.settings.transforms : [];
    const setVars: Record<string, Field> = {};
    let body: Contribution["body"];
    for (const raw of steps) {
      const step = raw as { setBody?: unknown; setVar?: unknown; value?: unknown };
      if (typeof step.setVar === "string" && step.setVar) {
        const shape =
          typeof step.value === "string" ? shapeOfExpression(step.value, scope) : undefined;
        setVars[step.setVar] = field(shape ?? DYN, "inferred", "set by multi-transform");
      } else if (typeof step.setBody === "string") {
        const shape = shapeOfExpression(step.setBody, scope);
        body = shape ? { kind: "shape", shape } : "opaque";
      }
    }
    return { setVars, ...(body ? { body } : {}) };
  },

  // The sub-flow runs on a copy; only these two settings carry anything back. Both
  // hold CEL, evaluated against the ENRICHED message rather than this scope — so the
  // expressions are read as literals only, and a path in one is left unresolved.
  enrich: (block) => {
    const setVars: Record<string, Field> = {};
    const vars = block.settings.setVars;
    if (vars && typeof vars === "object") {
      for (const [name, expression] of Object.entries(vars as Record<string, unknown>)) {
        const shape = typeof expression === "string" ? shapeOfExpression(expression) : undefined;
        setVars[name] = field(shape ?? DYN, "inferred", "set by enrich");
      }
    }
    if (typeof block.settings.setBody !== "string") return { setVars };
    const shape = shapeOfExpression(block.settings.setBody);
    return { setVars, body: shape ? { kind: "shape", shape } : "opaque" };
  },

  // `as` names a variable, but only inside the body slot — see walk.ts, which seeds
  // it there. Downstream of the foreach it is gone, so nothing is contributed here.
  foreach: () => ({ body: "opaque" }),

  // Both rebuild the message from many, or from one split into many.
  split: () => ({ body: "opaque" }),
  aggregate: () => ({ body: "opaque" }),

  // Another flow ran. We do not follow it, so everything it may have done is unknown.
  "flow-ref": () => ({ body: "opaque" }),
};

/** Every block type the special table handles, for the rot alarm to exempt. */
export const SPECIAL_TYPES = new Set(Object.keys(SPECIAL));

/**
 * `scope` is what the block RECEIVES, so an expression naming a path can be resolved
 * against it. Omitted, path resolution is simply unavailable and literals still work.
 */
export function contributionOf(block: BlockNode, scope?: Scope): Contribution {
  const spec = getBlockSpec(block.type);
  // A block type the schema has never heard of could do anything at all.
  if (!spec) return { body: "opaque" };

  const conventional = varsFromConvention(block, spec);
  const special = SPECIAL[block.type]?.(block, spec, scope) ?? {};
  const intoBody = spec.fields.some(
    (f) => f.type === "string" && BODY_OR_VAR.has(f.name) && !value(block, f) && !SLOT_SCOPED[block.type]?.has(f.name),
  );
  const setVars = { ...conventional, ...(special.setVars ?? {}) };
  // A deleted name cannot also be a set one. delete-variable's setting is called
  // `name`, which the convention reads as a declaration — this is what settles it.
  for (const name of special.deleteVars ?? []) delete setVars[name];
  const body = special.body ?? (intoBody ? ("opaque" as const) : undefined);
  return { ...special, ...(body ? { body } : {}), setVars };
}
