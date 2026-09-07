"use client";

import { useConfirm } from "@/app/components/ConfirmDialog";
import { saveLlmSettings } from "@/app/model/siteSettings";
import { ApiKeyField, EncryptionWarning, Field, INPUT } from "./fields";
import { useAgentForm } from "./AgentSettingsForm";
import {
  LLM_PROVIDERS,
  modelForProviderChange,
  providerById,
} from "./llmProviders";

/**
 * The site-wide LLM provider: which provider and model the platform's own agent
 * reasons with, and the key it authenticates with.
 *
 * As with the email settings, the API key draft is never seeded from the server and
 * an empty draft means "keep the stored key".
 *
 * Presentational: the draft, what is stored and when it is written all live in
 * AgentSettingsForm, because they are the page's and not this section's. What
 * stays here is the provider list, the rule about what happens to the model when
 * the provider changes, and removing a stored key — which is immediate, because
 * a revocation deferred behind a Save is a key somebody believes is gone.
 */
export default function LlmSettingsManager() {
  const confirm = useConfirm();
  const { stored, draft, set, busy, loading, run } = useAgentForm();
  const settings = stored.llm;
  const provider = draft?.provider ?? LLM_PROVIDERS[0].id;
  const model = draft?.model ?? "";
  const apiKey = draft?.llmApiKey ?? "";

  // Changing the provider carries the model with it, because a model id belongs
  // to a provider: OpenRouter prefixes the vendor, so the same model is named
  // differently there. Only a model the previous provider had by default is
  // replaced — one somebody typed is theirs and is kept.
  const changeProvider = (next: string) => {
    set("model", modelForProviderChange(model, next));
    set("provider", next);
  };

  const removeKey = async () => {
    // Refused while the model is empty for the same reason the save is: removing
    // writes the rest of the row, so clearing Model and pressing Remove would
    // send a state the Save button is refusing to send.
    if (settings === null || busy || model.trim() === "") return;
    const ok = await confirm({
      title: "Remove the stored API key?",
      body: "The platform agent will not be able to reach the provider until a new key is saved.",
      confirmLabel: "Remove",
      danger: true,
    });
    if (!ok) return;
    // Removing writes the rest of the row too, so it sends what is drafted
    // rather than what is stored — otherwise removing a key would silently
    // revert an unsaved model change beside it.
    await run(async () => {
      await saveLlmSettings({ provider, model: model.trim(), apiKey: "" });
      set("llmApiKey", "");
    });
  };

  const encryptionAvailable = settings?.encryptionAvailable ?? true;

  return (
    <section aria-labelledby="llm-heading" className="p-5">
      <h3 id="llm-heading" className="text-sm font-semibold">
        LLM provider
      </h3>
      <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
        The model the agent reasons with, and the one thing he cannot run
        without. This is separate from the keys an integration configures on its
        own LLM connectors &mdash; those are unaffected by anything here.
      </p>

      {!encryptionAvailable && <EncryptionWarning />}

      <div className="mt-4 flex flex-col gap-3">
        <Field label="Provider">
          <select
            value={provider}
            disabled={busy || loading}
            onChange={(e) => changeProvider(e.target.value)}
            className={`${INPUT} w-full`}
          >
            {LLM_PROVIDERS.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
              </option>
            ))}
          </select>
        </Field>

        <Field
          label="Model"
          hint="Free text, so a newly released model works without an update here."
        >
          <input
            value={model}
            disabled={busy || loading}
            placeholder={providerById(provider).defaultModel}
            onChange={(e) => set("model", e.target.value)}
            className={`${INPUT} w-full font-mono`}
          />
        </Field>

        <ApiKeyField
          value={apiKey}
          onChange={(v) => set("llmApiKey", v)}
          configured={settings?.configured ?? false}
          last4={settings?.last4 ?? ""}
          disabled={busy || loading || !encryptionAvailable}
          placeholder={providerById(provider).keyPlaceholder}
          onRemove={removeKey}
        />
      </div>
    </section>
  );
}
