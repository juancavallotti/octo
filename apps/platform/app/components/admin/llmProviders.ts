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
