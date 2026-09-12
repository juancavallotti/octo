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
  if (!source?.connector || !source.type) return null;
  const spec = getSourceSpec(source.connector, source.type);
  const field = spec?.fields.find((f) => f.type === "cel" && f.name === "payload");
  if (!field) return null;
  const expression = source.settings[field.name];
  return typeof expression === "string" && expression.trim() !== "" ? expression : null;
}
