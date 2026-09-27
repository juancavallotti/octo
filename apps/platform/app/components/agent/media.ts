/**
 * What each LLM connector can be sent, as the chat panel needs to know it.
 *
 * A **hint**, and deliberately labelled one. The authority is the runtime: each
 * provider connector reports what its configured model reads, an ai-agent
 * configured to send files against a text-only one fails to build, and a type a
 * connector cannot encode fails the call. This table exists only so the composer
 * can decide whether to offer a paperclip, which it has to do before any of that
 * has happened.
 *
 * It is kept beside the connector types rather than fetched because the
 * orchestrator is a separate Go module that imports none of the runtime's
 * packages, so there is no shared table to read. Keep it in step with
 * runtime/connectors/llm/*; a row that drifts wide offers a file that is refused
 * on the first turn, and one that drifts narrow hides a file the model would
 * have read.
 *
 * A `type/` entry stands for the whole family, exactly as the Gemini connector's
 * own list does. That is not a shorthand — it is what keeps the narrow drift from
 * being inevitable: the connector accepts `video/` by prefix, so any enumeration
 * here is a list of the video types somebody happened to think of, and
 * video/quicktime was the one it cost. Browsers read `video/*` natively in a file
 * input's accept attribute, so the same entry serves both jobs.
 */

/** Connector types, as the orchestrator reports them on the agent's status. */
export const MEDIA_BY_CONNECTOR: Record<string, readonly string[]> = {
  "llm-anthropic": ["image/jpeg", "image/png", "image/gif", "image/webp", "application/pdf", "text/plain"],
  "llm-openai": ["image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf"],
  // Families, mirroring the connector's own list: it accepts image/, audio/ and
  // video/ by prefix, and enumerating them here is how a .mov gets turned away by
  // a picker for a model that would have read it.
  "llm-gemini": ["image/", "audio/", "video/", "application/pdf", "text/plain"],
  "llm-openrouter": ["image/png", "image/jpeg", "image/webp", "application/pdf"],
};

/**
 * What a site running `connectorType` may attach, or nothing when this build has
 * no row for it.
 *
 * An unknown connector reports none rather than guessing. Offering a file that
 * is then refused is worse than not offering one: the refusal arrives after the
 * upload, as a failed run.
 */
export function acceptedFor(connectorType: string | undefined): readonly string[] {
  if (!connectorType) return [];
  return MEDIA_BY_CONNECTOR[connectorType] ?? [];
}
