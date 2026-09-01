import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import AgentSettingsManager from "@/app/components/admin/AgentSettingsManager";
import LlmSettingsManager from "@/app/components/admin/LlmSettingsManager";
import WebSearchSettingsManager from "@/app/components/admin/WebSearchSettingsManager";

/**
 * The platform agent (`/platform/admin/agent`), whole.
 *
 * These were separate tabs — LLM provider and Platform agent — and they were two
 * descriptions of one task. Nothing else on this installation configures an LLM
 * provider; it exists so that Dr. Octo can reason. Splitting them made the one
 * requirement invisible, since the page that refuses to install him was not the
 * page holding the key he is refused for.
 *
 * So: the provider first, then web search, then the deployment, in the order they
 * are needed — and in decreasing order of how much they matter, since the middle
 * one is optional and the first one is not. The
 * install button is disabled until the LLM provider is configured — the
 * orchestrator decides that, not this page — and its reason now links to a section
 * a scroll away rather than to somewhere else entirely.
 *
 * Embeddings are NOT here, and were briefly. They configure how agent memory is
 * searched, not how the agent reasons, and they are not configured on this
 * platform at all — they are chart values on the embedding server. What was left
 * was a read-only report about search, which belongs on the page where someone
 * searches: see SearchRanking on /platform/memory.
 *
 * ConfirmProvider wraps both because each asks before something irreversible:
 * removing a stored key, and removing or rolling out the agent.
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

          <LlmSettingsManager />

          <WebSearchSettingsManager />

          <AgentSettingsManager />
        </div>
      </div>
    </ConfirmProvider>
  );
}
