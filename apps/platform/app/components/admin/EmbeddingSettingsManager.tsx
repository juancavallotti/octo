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
   * Changing the model is the one setting here that a second change cannot undo.
   *
   * Stored vectors carry no record of which model produced them, so a table
   * holding two models' vectors is not searchable either way. The platform does
   * not re-embed, so the honest thing is to say what will happen before it does —
   * and only when there is something to lose.
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
        `${embedded.toLocaleString()} stored ${embedded === 1 ? "item" : "items"} were ` +
        "vectorized with the current model. Vectors from two different models cannot be " +
        "compared, so semantic search will return unreliable results until everything is " +
        "re-embedded — which the platform does not do for you. Clear the settings first " +
        "if you want a clean backfill.",
      confirmLabel: "Change anyway",
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
        "Search falls back to full-text matching immediately. Stored vectors are left " +
        "where they are, so turning the same model back on does not mean re-embedding " +
        "everything.",
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

/**
 * How much of the store has a vector.
 *
 * It is on the page because turning embeddings on does not make search semantic
 * — it makes it become semantic, over however long the backlog takes. Without
 * this an operator who configures a provider and searches immediately finds the
 * same results as before and reasonably concludes it did not work.
 */
function BackfillProgress({ embedded, pending }: { embedded: number; pending: number }) {
  const total = embedded + pending;
  if (total === 0) return null;
  const done = Math.round((embedded / total) * 100);
  return (
    <div className="mt-4 rounded-lg border border-black/10 p-4 dark:border-white/10">
      <h2 className="text-sm font-medium">Backfill</h2>
      <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
        {pending === 0
          ? `All ${embedded.toLocaleString()} stored items are vectorized.`
          : `${embedded.toLocaleString()} of ${total.toLocaleString()} vectorized. ` +
            "The rest are searched by text until the sweep reaches them."}
      </p>
      <div
        className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-black/10 dark:bg-white/10"
        role="progressbar"
        aria-valuenow={done}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label="Embedding backfill progress"
      >
        <div className="h-full bg-emerald-500" style={{ width: `${done}%` }} />
      </div>
    </div>
  );
}
