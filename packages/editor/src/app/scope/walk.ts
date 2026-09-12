import type { BlockNode, EditorDocument, FlowDoc } from "../model/document";
import { contributionOf } from "./contributions";
import { DYN, field, mergeFields, objectOf } from "./shape";
import { rootScope, sourceScope } from "./seed";
import type { CelSite, Field, Scope } from "./types";

/**
 * One walk of the document, producing the scope every CEL expression sits in.
 *
 * Each block's *incoming* scope is recorded — the message as that block receives it.
 * That is what a block's own settings reference, and it is also what a mock's `when`
 * is evaluated against, so one map serves both.
 */

export interface ScopeInputs {
  doc: EditorDocument;
  /** Saved test inputs by flow id, for seeding `body` and `vars`. */
  inputs?: ReadonlyMap<string, { data?: string; vars?: string }[]>;
}

export interface ScopeIndex {
  /** Incoming scope by block id. */
  blocks: Map<string, Scope>;
  /** Scope at the END of a flow — what its result expressions see. */
  flowOutputs: Map<string, Scope>;
  /** Scope for a flow's source payload expression, which is not a message scope. */
  sources: Map<string, Scope>;
  /**
   * The scope with no position, and the fallback for a site the index does not know.
   *
   * Held on the index rather than recomputed from the document at lookup time so that
   * resolving a site needs nothing but the index — which is what lets a CEL field ask
   * for its scope without also reaching for the editor's state.
   */
  document: Scope;
}

/** Apply one block's contribution, returning the scope the NEXT block receives. */
function advance(scope: Scope, block: BlockNode): Scope {
  const contribution = contributionOf(block);
  const roots = { ...scope.roots };

  if (contribution.body === "opaque") {
    // The block replaced the body with something we cannot describe. Keeping the old
    // keys would be worse than knowing nothing: they are now confidently wrong.
    roots.body = field(DYN, "declared", `replaced by ${block.type}`);
  }

  const vars = roots.vars;
  const known = vars?.shape.kind === "object" ? { ...vars.shape.fields } : {};
  for (const name of contribution.deleteVars ?? []) delete known[name];
  for (const [name, f] of Object.entries(contribution.setVars ?? {})) known[name] = f;
  roots.vars = { ...(vars ?? field(objectOf({}), "declared")), shape: objectOf(known) };

  return { roots };
}

/** Merge two scopes into what is true on both paths. */
function join(a: Scope, b: Scope): Scope {
  const roots: Record<string, Field> = {};
  for (const key of new Set([...Object.keys(a.roots), ...Object.keys(b.roots)])) {
    const left = a.roots[key];
    const right = b.roots[key];
    if (!left || !right) {
      roots[key] = { ...(left ?? right)!, certain: false };
      continue;
    }
    roots[key] = mergeFields(left, right);
  }
  return { roots };
}

/** Seed a composite's slot with whatever that slot puts in scope beyond its parent. */
function slotSeed(block: BlockNode, slot: string, scope: Scope): Scope {
  // foreach's `as` names the current element, and only within its body.
  if (block.type === "foreach" && slot === "body") {
    const name = (block.settings.as as string) || "item";
    const vars = scope.roots.vars;
    const known = vars?.shape.kind === "object" ? { ...vars.shape.fields } : {};
    known[name] = field(DYN, "inferred", "the current element");
    return { roots: { ...scope.roots, vars: { ...vars!, shape: objectOf(known) } } };
  }
  return scope;
}

/**
 * Whether what happens inside a slot is visible after the composite.
 *
 * `enrich` runs its body on a copy and carries back only what its `setBody` and
 * `setVars` settings say (see contributions.ts), so treating its branch as one more
 * path through the flow would leak every variable it set internally. `foreach`
 * iterates a body per element and the flow continues with the aggregate, not with
 * the last element's message.
 */
const ISOLATED_SLOTS = new Set(["enrich", "foreach"]);

export function buildIndex({ doc, inputs }: ScopeInputs): ScopeIndex {
  const index: ScopeIndex = {
    blocks: new Map(),
    flowOutputs: new Map(),
    sources: new Map(),
    document: rootScope(doc, null, undefined),
  };

  /** Walk a chain, recording each block's incoming scope; return the scope after it. */
  const walkChain = (blocks: BlockNode[], entry: Scope): Scope => {
    let scope = entry;
    for (const block of blocks) {
      index.blocks.set(block.id, scope);

      const branchEnds: Scope[] = [];
      for (const [slot, subs] of Object.entries(block.slots ?? {})) {
        for (const sub of subs) {
          const end = walkFlow(sub, slotSeed(block, slot, scope));
          if (!ISOLATED_SLOTS.has(block.type)) branchEnds.push(end);
        }
      }

      const after = advance(scope, block);
      // A composite whose branches are paths through the flow continues with what
      // every branch agrees on — plus the fact that a branch may not have run, which
      // is why `after` is in the join rather than replaced by it.
      scope = branchEnds.reduce(join, after);
    }
    return scope;
  };

  /** Walk a flow's process chain (and its error chain), recording its end scope. */
  const walkFlow = (flow: FlowDoc, entry: Scope): Scope => {
    const end = walkChain(flow.process, entry);
    index.flowOutputs.set(flow.id, end);
    // The error chain starts from the flow's entry, not from wherever it failed: what
    // the failing block had done is exactly what is not knowable.
    if (flow.error) walkChain(flow.error.process, entry);
    return end;
  };

  for (const flow of doc.flows) {
    index.sources.set(flow.id, sourceScope());
    walkFlow(flow, rootScope(doc, flow, inputs?.get(flow.id)));
  }
  return index;
}

/**
 * The scope for one CEL site.
 *
 * A site the index does not know — a block deleted while its settings were open —
 * falls back to the document scope: every message variable, none of them described.
 * That is also what a template or the CEL tester gets, since neither has a position.
 */
export function scopeAt(index: ScopeIndex, site: CelSite): Scope {
  switch (site.kind) {
    case "block":
      return index.blocks.get(site.blockId) ?? index.document;
    case "flow-output":
      return index.flowOutputs.get(site.flowId) ?? index.document;
    case "source":
      return index.sources.get(site.flowId) ?? sourceScope();
    case "document":
      return index.document;
  }
}
