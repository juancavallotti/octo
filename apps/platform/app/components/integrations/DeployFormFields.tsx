"use client";

import { Globe, Tag } from "lucide-react";
import type { DeployEnvVar } from "@/app/model/orchestrator";

import { INPUT } from "./inputStyles";
import SlugField from "./SlugField";
import DeployEnvFields, { type EnvBinding } from "./DeployEnvFields";
import Field from "./Field";
import TracingToggle from "./TracingToggle";
import AdvancedDeployFields from "./AdvancedDeployFields";

/**
 * The deploy dialog's scrolling body: version, scale, address, tracing, env, and
 * the advanced settings (the platform-access grants and the runner).
 *
 * Every one of these fields is already its own component — SlugField,
 * ReplicaStepper, TracingToggle, DeployEnvFields — and this is the container that
 * arranges and labels them. Splitting it from the modal leaves that file as
 * chrome and submit, which is a shape the folder already uses.
 */
export default function DeployFormFields({
  integrationId,
  currentMode,
  activeVersionLabel,
  loading,
  newTag,
  onNewTag,
  replicas,
  onReplicas,
  networked,
  expose,
  onExpose,
  slug,
  onSlug,
  onSlugOk,
  tracing,
  onTracing,
  orchestratorApi,
  onOrchestratorApi,
  observabilityApi,
  onObservabilityApi,
  runner,
  onRunner,
  envVars,
  bindings,
  secretNames,
  providedKeys,
  missingRequired,
  onBinding,
  busy,
  error,
}: {
  integrationId: string;
  /** Deploying the working copy, which has to be tagged first. */
  currentMode: boolean;
  /** The version's options have not arrived, so the address section cannot yet
   *  say whether there is an address to configure. */
  loading: boolean;
  /** The tag being deployed, when one is active. */
  activeVersionLabel: string;
  newTag: string;
  onNewTag: (tag: string) => void;
  replicas: number;
  onReplicas: (n: number) => void;
  /** The definition declares HTTP_PORT, so it gets an address. */
  networked: boolean;
  expose: boolean;
  onExpose: (on: boolean) => void;
  slug: string;
  onSlug: (slug: string) => void;
  onSlugOk: (ok: boolean) => void;
  tracing: boolean;
  onTracing: (on: boolean) => void;
  orchestratorApi: boolean;
  onOrchestratorApi: (on: boolean) => void;
  observabilityApi: boolean;
  onObservabilityApi: (on: boolean) => void;
  /** Which runner image the pods run: "" (the default) or "agentic". */
  runner: string;
  onRunner: (runner: string) => void;
  envVars: DeployEnvVar[];
  bindings: Record<string, EnvBinding>;
  secretNames: string[];
  providedKeys: ReadonlySet<string>;
  missingRequired: string[];
  onBinding: (name: string, patch: Partial<EnvBinding>) => void;
  busy: boolean;
  error: string | null;
}) {
  return (
    <div className="flex max-h-[70vh] flex-col gap-5 overflow-y-auto px-4 py-4">
      {currentMode ? (
        <Field
          label="New version"
          hint="Deploying Current tags the working copy first, then ships that frozen version."
        >
          <div className="flex items-center gap-2">
            <Tag size={14} className="shrink-0 text-zinc-400" />
            <input
              value={newTag}
              disabled={busy}
              placeholder="e.g. v1.0.0"
              onChange={(e) => onNewTag(e.target.value)}
              className={`${INPUT} w-full`}
            />
          </div>
        </Field>
      ) : (
        <Field
          label="Version"
          hint="Ships this tag's frozen definition. Pick a different version from the header."
        >
          <span className="inline-flex items-center gap-1.5 rounded-md bg-sky-500/10 px-2 py-1 text-sm font-medium text-sky-600 dark:text-sky-400">
            <Tag size={14} />
            {activeVersionLabel}
          </span>
        </Field>
      )}

      <Field
        label="Scale"
        hint="Runtime pods load-balanced behind the service."
      >
        <label className="flex items-center gap-2 text-sm text-zinc-600 dark:text-zinc-300">
          Replicas
          <input
            type="number"
            min={1}
            value={replicas}
            disabled={busy}
            onChange={(e) =>
              onReplicas(Math.max(1, Number(e.target.value) || 1))
            }
            className={`${INPUT} w-20`}
          />
        </label>
      </Field>

      {loading ? (
        <p className="text-sm text-zinc-400">Loading options…</p>
      ) : networked ? (
        <Field
          label="Address"
          hint={`Reachable in-cluster at octo-int-${slug.trim() || "{slug}"}. Must be unique.`}
        >
          <SlugField
            integrationId={integrationId}
            value={slug}
            onChange={onSlug}
            expose={expose}
            busy={busy}
            onValidChange={onSlugOk}
          />
          <label className="flex items-center gap-2 text-sm text-zinc-600 dark:text-zinc-300">
            <input
              type="checkbox"
              checked={expose}
              disabled={busy}
              onChange={(e) => onExpose(e.target.checked)}
              className="accent-sky-500"
            />
            <Globe size={14} />
            Expose externally at this address
          </label>
        </Field>
      ) : (
        <p className="text-sm text-zinc-400">
          No HTTP source — this integration runs as an internal workload with no
          address.
        </p>
      )}

      <Field
        label="Tracing"
        hint="Records every flow, block and model call, with what each one cost. For troubleshooting only — it significantly reduces throughput, so trace the deployment you are debugging and switch it off afterwards."
      >
        <TracingToggle busy={busy} checked={tracing} onChange={onTracing} />
      </Field>

      {envVars.length > 0 && (
        <Field
          label="Environment"
          hint="Fill each variable with a value or a cluster secret. Required ones are marked *."
        >
          <DeployEnvFields
            envVars={envVars}
            bindings={bindings}
            secretNames={secretNames}
            providedKeys={providedKeys}
            busy={busy}
            onChange={onBinding}
          />
          {missingRequired.length > 0 && (
            <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
              Provide a value or secret for:{" "}
              <span className="font-mono">{missingRequired.join(", ")}</span>
            </p>
          )}
        </Field>
      )}

      {/* Last, and collapsed: almost no integration needs either grant, and the
          ones that do are written for it. */}
      <AdvancedDeployFields
        busy={busy}
        orchestratorApi={orchestratorApi}
        onOrchestratorApi={onOrchestratorApi}
        observabilityApi={observabilityApi}
        onObservabilityApi={onObservabilityApi}
        runner={runner}
        onRunner={onRunner}
      />

      {error && <p className="text-sm text-red-500">{error}</p>}
    </div>
  );
}
