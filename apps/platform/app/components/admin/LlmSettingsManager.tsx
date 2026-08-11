"use client";

import { useCallback, useEffect, useState } from "react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  getLlmSettings,
  saveLlmSettings,
  type LlmSettings,
} from "@/app/model/siteSettings";
import { ApiKeyField, EncryptionWarning, Field, INPUT, SaveButton } from "./fields";
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
 */
export default function LlmSettingsManager() {
  const confirm = useConfirm();
  const [settings, setSettings] = useState<LlmSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const [provider, setProvider] = useState(LLM_PROVIDERS[0].id);
  const [model, setModel] = useState(LLM_PROVIDERS[0].defaultModel);
  const [apiKey, setApiKey] = useState("");

  const apply = useCallback((next: LlmSettings) => {
    setSettings(next);
    // An unconfigured site has neither, so fall back to the first provider and its
    // default model rather than showing an empty form.
    setProvider(next.provider || LLM_PROVIDERS[0].id);
    setModel(next.model || providerById(next.provider).defaultModel);
  }, []);

  // Shaped as a promise chain rather than an async body so nothing sets state in the
  // synchronous part of the effect below — the same form the other managers use.
  const load = useCallback(
    () => getLlmSettings().then(apply, (e) => setError((e as Error).message)),
    [apply],
  );

  useEffect(() => {
    load();
  }, [load]);

  /** Run a mutation, then refresh; surface failures inline. */
  const run = useCallback(
    async (fn: () => Promise<unknown>) => {
      setBusy(true);
      setError(null);
      setSaved(false);
      try {
        await fn();
        await load();
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [load],
  );

  const changeProvider = (next: string) => {
    setModel((current) => modelForProviderChange(current, next));
    setProvider(next);
  };

  const canSave = !busy && model.trim().length > 0;

  const save = () => {
    if (!canSave) return;
    run(async () => {
      await saveLlmSettings({
        provider,
        model,
        ...(apiKey ? { apiKey } : {}),
      });
      setApiKey("");
      setSaved(true);
    });
  };

  const removeKey = async () => {
    const ok = await confirm({
      title: "Remove the stored API key?",
      body: "The platform agent will not be able to reach the provider until a new key is saved.",
      confirmLabel: "Remove",
      danger: true,
    });
    if (!ok) return;
    run(async () => {
      await saveLlmSettings({ provider, model, apiKey: "" });
      setApiKey("");
    });
  };

  const encryptionAvailable = settings?.encryptionAvailable ?? true;

  return (
    <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
      <div className="mx-auto w-full max-w-2xl">
        <h1 className="text-lg font-semibold">LLM provider</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          The model the platform&rsquo;s own agent reasons with. This is separate
          from the keys an integration configures on its own LLM connectors — those
          are unaffected by anything here.
        </p>

        {!encryptionAvailable && <EncryptionWarning />}
        {error && <p className="mt-3 text-sm text-red-500">{error}</p>}
        {saved && !error && (
          <p className="mt-3 text-sm text-emerald-600 dark:text-emerald-400">
            Settings saved.
          </p>
        )}

        <div className="mt-4 flex flex-col gap-3 rounded-lg border border-black/10 p-4 dark:border-white/10">
          <Field label="Provider">
            <select
              value={provider}
              disabled={busy}
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
              disabled={busy}
              placeholder={providerById(provider).defaultModel}
              onChange={(e) => setModel(e.target.value)}
              className={`${INPUT} w-full font-mono`}
            />
          </Field>

          <ApiKeyField
            value={apiKey}
            onChange={setApiKey}
            configured={settings?.configured ?? false}
            last4={settings?.last4 ?? ""}
            disabled={busy || !encryptionAvailable}
            placeholder={providerById(provider).keyPlaceholder}
            onRemove={removeKey}
          />

          <SaveButton onClick={save} disabled={!canSave} />
        </div>
      </div>
    </div>
  );
}
