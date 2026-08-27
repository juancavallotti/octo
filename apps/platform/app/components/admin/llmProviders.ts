/**
 * The providers the runtime can talk to, and the model each one defaults to.
 *
 * Duplicated from the runtime connectors — runtime/core/llm.go for the provider
 * names, and the `defaultModel` in
 * runtime/connectors/llm/{anthropic,openai,gemini,openrouter} for the models. There
 * is no seam to share them through: the runtime is a separate Go module from the
 * orchestrator, which is a separate artifact again from this bundle. If a provider
 * or default changes there, it changes here.
 *
 * The model is a free-text field rather than a dropdown on purpose. Model names turn
 * over faster than releases of this app, and the connectors themselves treat it as a
 * plain string with a default — a fixed list here would be stale within months and
 * would block anyone pointing at a model we have not heard of.
 */

export interface LlmProvider {
  /** The value stored and sent to the orchestrator. */
  id: string;
  label: string;
  defaultModel: string;
  /** Shown in the key field, to hint at the shape without prescribing it. */
  keyPlaceholder: string;
}

export const LLM_PROVIDERS: LlmProvider[] = [
  {
    id: "ANTHROPIC",
    label: "Anthropic",
    defaultModel: "claude-sonnet-4-6",
    keyPlaceholder: "sk-ant-...",
  },
  {
    id: "OPENAI",
    label: "OpenAI",
    defaultModel: "gpt-5.4",
    keyPlaceholder: "sk-...",
  },
  {
    id: "GOOGLE",
    label: "Google",
    defaultModel: "gemini-3.5-flash",
    keyPlaceholder: "AIza...",
  },
  {
    // The one entry that is not a single vendor: OpenRouter fronts hundreds of
    // models, so its ids carry the vendor as a prefix and switching model never
    // means switching key.
    id: "OPENROUTER",
    label: "OpenRouter",
    defaultModel: "anthropic/claude-sonnet-4.5",
    keyPlaceholder: "sk-or-v1-...",
  },
];

/** The default model set of every provider, for recognising an untouched value. */
const DEFAULT_MODELS = new Set(LLM_PROVIDERS.map((p) => p.defaultModel));

export function providerById(id: string): LlmProvider {
  return LLM_PROVIDERS.find((p) => p.id === id) ?? LLM_PROVIDERS[0];
}

/**
 * The model to show after switching provider. A model the user typed themselves is
 * kept — switching provider should not silently discard it — while an empty field or
 * one still holding another provider's default is replaced, since that value means
 * nothing for the newly chosen provider.
 */
export function modelForProviderChange(
  currentModel: string,
  nextProvider: string,
): string {
  const trimmed = currentModel.trim();
  if (trimmed === "" || DEFAULT_MODELS.has(trimmed)) {
    return providerById(nextProvider).defaultModel;
  }
  return currentModel;
}

/**
 * The providers with an embeddings endpoint, and the model each defaults to.
 *
 * Anthropic is absent and always will be — it has no embeddings API. That is
 * stated rather than left as an omission because it is in the list above, so its
 * absence here reads as a mistake unless someone says otherwise.
 *
 * Unlike the LLM list, the model is not entirely free: agent_turns.embedding is
 * vector(1536), because an indexable vector column needs a fixed width, so the
 * chosen model has to emit 1536 dimensions. The defaults below all can — natively
 * for text-embedding-3-small, through the provider's output-dimension parameter
 * for the larger ones and for Gemini — and a model that cannot is refused by the
 * orchestrator on its first call rather than stored as vectors nothing can search.
 */
export const EMBEDDING_PROVIDERS: LlmProvider[] = [
  {
    id: "OPENAI",
    label: "OpenAI",
    defaultModel: "text-embedding-3-small",
    keyPlaceholder: "sk-...",
  },
  {
    id: "GOOGLE",
    label: "Google",
    defaultModel: "text-embedding-004",
    keyPlaceholder: "AIza...",
  },
  {
    id: "OPENROUTER",
    label: "OpenRouter",
    defaultModel: "openai/text-embedding-3-small",
    keyPlaceholder: "sk-or-v1-...",
  },
];

/** The stored vector width. Mirrors embedding.Dimensions in the orchestrator. */
export const EMBEDDING_DIMENSIONS = 1536;

const EMBEDDING_DEFAULT_MODELS = new Set(EMBEDDING_PROVIDERS.map((p) => p.defaultModel));

/** Look up an embedding provider, falling back to the first for an unknown id. */
export function embeddingProviderById(id: string): LlmProvider {
  return EMBEDDING_PROVIDERS.find((p) => p.id === id) ?? EMBEDDING_PROVIDERS[0];
}

/** {@link modelForProviderChange} for the embedding list. */
export function embeddingModelForProviderChange(
  currentModel: string,
  nextProvider: string,
): string {
  const trimmed = currentModel.trim();
  if (trimmed === "" || EMBEDDING_DEFAULT_MODELS.has(trimmed)) {
    return embeddingProviderById(nextProvider).defaultModel;
  }
  return currentModel;
}
