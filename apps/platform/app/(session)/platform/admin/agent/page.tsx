import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import AgentSettingsManager from "@/app/components/admin/AgentSettingsManager";
import EmbeddingStatusPanel from "@/app/components/admin/EmbeddingStatusPanel";
import LlmSettingsManager from "@/app/components/admin/LlmSettingsManager";

/**
 * The platform agent (`/platform/admin/agent`), whole.
 *
 * These were three tabs — LLM provider, Embeddings, Platform agent — and they were
 * three descriptions of one task. Nothing else configures an LLM provider here, and
 * nothing else reads the embedding model: both exist so that Dr. Octo can reason
 * and so that what he remembers can be searched by meaning. Splitting them made the
 * one requirement invisible, since the page that refuses to install him was not the
 * page holding the key he is refused for.
 *
 * So: models first, deployment second, in the order they are needed. The install
 * button is disabled until the LLM provider is configured — the orchestrator
 * decides that, not this page — and its reason now links to a section a scroll
 * away rather than to somewhere else entirely.
 *
 * ConfirmProvider wraps the lot because all three ask before something
 * irreversible: removing a stored key, changing the embedding model once anything
 * is embedded, and removing or rolling out the agent.
 */
export default function AdminAgentPage() {
  return (
    <ConfirmProvider>
      <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
        <div className="mx-auto w-full max-w-2xl">
          <h1 className="text-lg font-semibold">Platform agent</h1>
          <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
            Dr. Octo answers questions about this installation and helps build
            integrations. What he reasons with is configured here, and so is he.
          </p>

          <section aria-labelledby="models-heading" className="mt-6">
            <h2 id="models-heading" className="text-base font-semibold">
              Models
            </h2>
            <LlmSettingsManager />
            <EmbeddingStatusPanel />
          </section>

          <AgentSettingsManager />
        </div>
      </div>
    </ConfirmProvider>
  );
}
