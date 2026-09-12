import { getSourceSpec } from "../schema";
import type { SourceNode } from "./document";

/**
 * The CEL expression a source is configured to synthesize its message body from, if it
 * has one.
 *
 * Found through the schema rather than by knowing which sources work this way: a source
 * declares a `cel` field named `payload` when the body is its to make (cron), and does
 * not when the body arrives from outside (an HTTP request). So this stays right as
 * sources are added, and it is not a mirror of anything in the runtime — it reads the
 * user's own expression out of the document they wrote.
 *
 * Shared because three places want it and all three want the same answer: the scope
 * model completes against the shape it implies, the ▶ menu offers running with it, and
 * the background shape-learner uses it as the input for a flow that has no saved one.
 */
export function sourcePayloadExpression(source: SourceNode | undefined): string | null {
  if (!source?.connector) return null;
  // A source may name its connector and leave `type` out — the runtime resolves a
  // config-less connector by type on demand, so `cron:` with no type IS the cron
  // source, and sourceFromRuntime preserves the omission rather than filling it in.
  // Falling back to the connector name can only ever find a spec that exists: a wrong
  // guess finds none and we answer null exactly as before.
  const spec = getSourceSpec(source.connector, source.type ?? source.connector);
  const field = spec?.fields.find((f) => f.type === "cel" && f.name === "payload");
  if (!field) return null;
  const expression = source.settings[field.name];
  return typeof expression === "string" && expression.trim() !== "" ? expression : null;
}
