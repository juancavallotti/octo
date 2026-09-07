"use client";

import { useState } from "react";
import { PrimaryButton } from "./fields";
import { useAgentForm, type AgentDraft } from "./AgentSettingsForm";
import {
  saveLlmSettings,
  saveWebSearchSettings,
} from "@/app/model/siteSettings";
import { setAgentDeploymentSettings } from "@/app/model/agent";

/**
 * The one Save.
 *
 * It writes only what changed, in a deliberate order: the provider first,
 * because it is the thing the agent cannot run without and the thing a
 * roll-out would carry; then the search key; then the two settings that live on
 * the pods, in a single call so they replace the pods once rather than twice.
 *
 * Saying what will happen matters more here than under the old per-section
 * buttons. "Apply" beside the turn limit was obviously about the deployment;
 * "Save" at the foot of a page is not, so when a pod-level field is dirty the
 * button says so before it is pressed.
 */
export default function AgentSaveBar({
  deployed,
  onSaved,
}: {
  /** Pod settings can only be applied to something that exists. */
  deployed: boolean;
  onSaved: () => Promise<void> | void;
}) {
  const { draft, dirty, committed } = useAgentForm();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!draft) return null;

  const rolls = dirty.deployment && deployed;

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      if (dirty.llm) {
        await saveLlmSettings({
          provider: draft.provider,
          model: draft.model.trim(),
          ...(draft.llmApiKey ? { apiKey: draft.llmApiKey } : {}),
        });
      }
      if (dirty.webSearch) {
        await saveWebSearchSettings({ apiKey: draft.webSearchApiKey });
      }
      if (rolls) {
        // Sent together, so the pods are replaced once. An empty turn limit is
        // zero, which the orchestrator reads as "no override" — the only way
        // back to the definition's own default.
        await setAgentDeploymentSettings({
          maxIterations: Number(draft.maxIterations.trim() || "0"),
          autoFix: draft.autoFix,
        });
      }
      // The secrets are cleared from the draft because they were write-only: what
      // is stored now is not something the page can show back.
      const saved: AgentDraft = {
        ...draft,
        llmApiKey: "",
        webSearchApiKey: "",
      };
      committed(saved);
      await onSaved();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mt-4 flex flex-wrap items-center gap-3">
      <PrimaryButton onClick={save} disabled={!dirty.any || busy}>
        {busy ? "Saving…" : "Save"}
      </PrimaryButton>
      {dirty.any && !busy && (
        <p className="text-xs text-zinc-500 dark:text-zinc-400">
          {rolls
            ? "Saving replaces the agent’s pods: the runtime reads these when it starts."
            : "Unsaved changes."}
        </p>
      )}
      {error && (
        <p role="alert" className="text-sm text-red-500">
          {error}
        </p>
      )}
    </div>
  );
}
