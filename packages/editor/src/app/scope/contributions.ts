import type { BlockNode } from "../model/document";
import { getBlockSpec } from "../schema";
import type { BlockSpec, FieldSpec } from "../schema/types";
import { DYN, field, objectOf } from "./shape";
import type { Contribution, Field, ValueShape } from "./types";

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
 * documents, and it decays the same way. The mitigation is the rot-alarm test in
 * contributions.test.ts: a new block with a variable-naming setting that neither tier
 * accounts for fails the suite.
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
const SPECIAL: Record<string, (block: BlockNode, spec: BlockSpec) => Contribution> = {
  // Its `name` is the variable, which the convention already reads. Listed so the
  // rot alarm does not have to special-case it.
  "set-variable": () => ({}),

  "delete-variable": (block) => {
    const name = block.settings.name;
    return typeof name === "string" && name ? { deleteVars: [name] } : {};
  },

  // The result of the call becomes the body; statusVar (convention) carries the code.
  rest: () => ({ body: "opaque" }),
  "rest-dynamic": () => ({ body: "opaque" }),

  "set-payload": () => ({ body: "opaque" }),
  "template-resource": () => ({ body: "opaque" }),

  // An ordered list of {setBody} / {setVar, value} steps — see TransformListEditor.
  "multi-transform": (block) => {
    const steps = Array.isArray(block.settings.transforms) ? block.settings.transforms : [];
    const setVars: Record<string, Field> = {};
    let body: Contribution["body"];
    for (const raw of steps) {
      const step = raw as { setBody?: unknown; setVar?: unknown };
      if (typeof step.setVar === "string" && step.setVar) {
        setVars[step.setVar] = field(DYN, "inferred", "set by multi-transform");
      } else if (step.setBody !== undefined) {
        body = "opaque";
      }
    }
    return { setVars, ...(body ? { body } : {}) };
  },

  // The sub-flow runs on a copy; only these two settings carry anything back.
  enrich: (block) => {
    const setVars: Record<string, Field> = {};
    const vars = block.settings.setVars;
    if (vars && typeof vars === "object") {
      for (const name of Object.keys(vars as Record<string, unknown>)) {
        setVars[name] = field(DYN, "inferred", "set by enrich");
      }
    }
    const body = block.settings.setBody ? ("opaque" as const) : undefined;
    return { setVars, ...(body ? { body } : {}) };
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

export function contributionOf(block: BlockNode): Contribution {
  const spec = getBlockSpec(block.type);
  // A block type the schema has never heard of could do anything at all.
  if (!spec) return { body: "opaque" };

  const conventional = varsFromConvention(block, spec);
  const special = SPECIAL[block.type]?.(block, spec) ?? {};
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
