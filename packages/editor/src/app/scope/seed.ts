import type { EditorDocument, FlowDoc } from "../model/document";
import type { MessageShape } from "./evidence";
import { DYN, field, merge, objectOf } from "./shape";
import type { Field, Scope, ValueShape } from "./types";

/**
 * What is in scope before any block has run.
 *
 * Three sources, weakest last: the runtime's own message variables, which are always
 * there; the source's declared settings, which say what an HTTP request will have
 * put in `vars`; and the flow's saved test inputs, which are the only honest evidence
 * about `body` the editor has without running anything.
 */

/** The variables the runtime puts in scope for every message expression. */
function messageRoots(env: ValueShape): Record<string, Field> {
  return {
    body: field(DYN, "declared", "the message body"),
    vars: field(objectOf({}), "declared", "message variables"),
    env: field(env, "declared", "declared environment variables"),
    eventID: field({ kind: "string" }, "declared", "this message's id"),
    correlationID: field({ kind: "string" }, "declared", "the correlation id"),
    now: field({ kind: "timestamp" }, "declared", "the current time"),
  };
}

/** `env` is the one near-closed object: the document declares exactly these names. */
function envShape(doc: EditorDocument): ValueShape {
  const fields: Record<string, Field> = {};
  for (const v of doc.env ?? []) {
    if (v.name) fields[v.name] = field({ kind: "string" }, "declared", "declared in Environment");
  }
  // Still open: an env file the editor cannot read may carry more.
  return objectOf(fields);
}

/** The variables an HTTP source is configured to set on every message it produces. */
function sourceVars(flow: FlowDoc | null): Record<string, Field> {
  const source = flow?.source;
  if (!source || source.connector !== "http") return {};

  const out: Record<string, Field> = {};
  const headers = source.settings.headers;
  if (Array.isArray(headers)) {
    for (const h of headers) {
      // The runtime copies a listed header in under its own name.
      if (typeof h === "string" && h) out[h] = field({ kind: "string" }, "inferred", "copied from the request header");
    }
  }
  const rawBodyVar = source.settings.rawBodyVar;
  if (typeof rawBodyVar === "string" && rawBodyVar) {
    out[rawBodyVar] = field({ kind: "string" }, "inferred", "the raw request body");
  }
  // Path parameters: `/orders/{id}` puts `id` in scope.
  const path = source.settings.path;
  if (typeof path === "string") {
    for (const [, name] of path.matchAll(/\{([A-Za-z_][A-Za-z0-9_]*)\}/g)) {
      out[name] = field({ kind: "string" }, "inferred", "a path parameter");
    }
  }
  return out;
}

/**
 * The scope at the top of a flow: the runtime's variables, refined by whatever the
 * workspace says these messages start as (see evidence.ts).
 */
export function rootScope(
  doc: EditorDocument,
  flow: FlowDoc | null,
  known: MessageShape | undefined,
): Scope {
  const roots = messageRoots(envShape(doc));

  const declared = sourceVars(flow);
  let varsShape: ValueShape = objectOf(declared);
  const bodyShape: ValueShape = known?.body ?? { kind: "unknown" };
  if (known?.vars) varsShape = merge(varsShape, known.vars);

  if (bodyShape.kind !== "unknown") {
    // Replaced, not merged. The declared seed is `dyn` — the absence of evidence —
    // and merging evidence with the absence of it widens straight back to `dyn`,
    // throwing away the only thing the editor actually knows about this body.
    roots.body = field(bodyShape, "sample", "seen in a test input");
  }
  roots.vars = { ...roots.vars, shape: varsShape };
  return { roots };
}

/**
 * The scope for a source's payload expression.
 *
 * Deliberately not the message scope: a source has produced no message yet, so
 * `body`, `vars` and the rest are not compiled into it (Go: SourcePayloadVars in
 * runtime/core/expr/source.go). Offering them here is how the editor came to suggest
 * names that cannot compile.
 */
export function sourceScope(): Scope {
  return {
    roots: {
      now: field({ kind: "timestamp" }, "declared", "the trigger time"),
      settings: field(objectOf({}), "declared", "the source's own settings"),
    },
  };
}
