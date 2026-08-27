"use client";

import { useCallback, useEffect, useState } from "react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  clearEmbeddingSettings,
  getEmbeddingSettings,
  saveEmbeddingSettings,
  type EmbeddingStatus,
} from "@/app/model/siteSettings";
import { ApiKeyField, EncryptionWarning, Field, INPUT, PrimaryButton } from "./fields";
import { BackfillProgress } from "./BackfillProgress";
import {
  EMBEDDING_DIMENSIONS,
  EMBEDDING_PROVIDERS,
  embeddingModelForProviderChange,
  embeddingProviderById,
} from "./llmProviders";

/**
 * The site-wide embedding configuration: what agent memory is vectorized with.
 *
 * Two things make this page different from the LLM one. It shows how far the
 * backfill has got, because configuring a provider and search actually being
 * semantic are not the same moment. And it asks before a model change once
 * anything has been embedded, because that is the one setting here that cannot be
 * undone by changing it back.
 */
export default function EmbeddingSettingsManager() {
  const confirm = useConfirm();
  const [status, setStatus] = useState<EmbeddingStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const [provider, setProvider] = useState(EMBEDDING_PROVIDERS[0].id);
  const [model, setModel] = useState(EMBEDDING_PROVIDERS[0].defaultModel);
  const [apiKey, setApiKey] = useState("");

  const apply = useCallback((next: EmbeddingStatus) => {
    setStatus(next);
    // Normalised through the provider list rather than taken as given, as the LLM
    // form does: an unconfigured site has no provider, and a stored one we no
    // longer offer would leave the select showing something a save would not send.
    const id = embeddingProviderById(next.settings.provider).id;
    setProvider(id);
    setModel(next.settings.model || embeddingProviderById(id).defaultModel);
  }, []);

  // A promise chain rather than an async body, so nothing sets state in the
  // synchronous part of the effect below.
  const load = useCallback(
    () => getEmbeddingSettings().then(apply, (e) => setError((e as Error).message)),
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
    setModel((current) => embeddingModelForProviderChange(current, next));
    setProvider(next);
  };

  // Gated on the settings having loaded, not just on the fields being valid: the
  // form seeds itself with a provider and model before the request resolves, so
  // without this a fast click saves those defaults over whatever was stored.
  const canSave = status !== null && !busy && model.trim().length > 0;
  const embedded = status?.embedded ?? 0;
  const pending = status?.pending ?? 0;
  const encryptionAvailable = status?.encryptionAvailable ?? true;

  /**
   * Changing the embedding space discards every stored vector.
   *
   * It has to. Vectors carry no record of which model produced them, so a store
   * holding two models' is not searchable either way — keeping one space at a time
   * is what makes that simplification safe rather than a corruption waiting for
   * someone to change a setting. The text is untouched and search keeps working on
   * it while the sweep rebuilds, so what this actually costs is the re-embedding.
   *
   * Said before it happens, and only when there is something to lose.
   */
  const confirmModelChange = async (): Promise<boolean> => {
    const changingModel = status !== null && status.settings.model !== "" &&
      status.settings.model !== model.trim();
    const changingProvider = status !== null && status.settings.provider !== "" &&
      status.settings.provider !== provider;
    if (!(changingModel || changingProvider) || embedded === 0) return true;
    return confirm({
      title: "Change the embedding model?",
      body:
        `${embedded.toLocaleString()} stored ${embedded === 1 ? "item" : "items"} ` +
        `${embedded === 1 ? "was" : "were"} vectorized with the current model, and will be ` +
        "discarded — vectors from two models cannot be compared, so the store keeps one " +
        "at a time. Nothing is lost but the vectors: search falls back to matching text " +
        "while the backfill rebuilds them, which costs whatever your provider charges " +
        "for the whole store again.",
      confirmLabel: "Change and re-embed",
      danger: true,
    });
  };

  const save = async () => {
    if (!canSave) return;
    if (!(await confirmModelChange())) return;
    run(async () => {
      await saveEmbeddingSettings({
        provider,
        model: model.trim(),
        ...(apiKey ? { apiKey } : {}),
      });
      setApiKey("");
      setSaved(true);
    });
  };

  const removeKey = async () => {
    const ok = await confirm({
      title: "Turn embeddings off?",
      body:
        "Search falls back to matching text immediately, and the stored vectors are " +
        "discarded — with no configuration there is nothing recording which model made " +
        "them, so keeping them would risk a different model being configured over the " +
        "top. Turning embeddings back on re-embeds the store.",
      confirmLabel: "Turn off",
      danger: true,
    });
    if (!ok) return;
    run(async () => {
      await clearEmbeddingSettings();
      setApiKey("");
    });
  };

  return (
    <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
      <div className="mx-auto w-full max-w-2xl">
        <h1 className="text-lg font-semibold">Embeddings</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          What agent memory is vectorized with. Configure a provider and searching an
          agent&rsquo;s conversations and remembered facts ranks by meaning rather than by
          words. Leave it unconfigured and search still works — it matches text.
        </p>

        {!encryptionAvailable && <EncryptionWarning />}
        {error && <p className="mt-3 text-sm text-red-500">{error}</p>}
        {saved && !error && (
          <p className="mt-3 text-sm text-emerald-600 dark:text-emerald-400">
            Settings saved.
          </p>
        )}

        <div className="mt-4 flex flex-col gap-3 rounded-lg border border-black/10 p-4 dark:border-white/10">
          <Field
            label="Provider"
            hint="Anthropic is absent because it has no embeddings API."
          >
            <select
              value={provider}
              disabled={busy}
              onChange={(e) => changeProvider(e.target.value)}
              className={`${INPUT} w-full`}
            >
              {EMBEDDING_PROVIDERS.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.label}
                </option>
              ))}
            </select>
          </Field>

          <Field
            label="Model"
            hint={
              `Must produce ${EMBEDDING_DIMENSIONS}-dimension vectors — that is the stored ` +
              "width. A model that produces another is refused rather than stored."
            }
          >
            <input
              value={model}
              disabled={busy}
              placeholder={embeddingProviderById(provider).defaultModel}
              onChange={(e) => setModel(e.target.value)}
              className={`${INPUT} w-full font-mono`}
            />
          </Field>

          <ApiKeyField
            value={apiKey}
            onChange={setApiKey}
            configured={status?.settings.configured ?? false}
            last4={status?.settings.last4 ?? ""}
            disabled={busy || !encryptionAvailable}
            placeholder={embeddingProviderById(provider).keyPlaceholder}
            onRemove={removeKey}
          />

          <PrimaryButton onClick={save} disabled={!canSave} />
        </div>

        <BackfillProgress embedded={embedded} pending={pending} />
      </div>
    </div>
  );
}
