"use client";

import Image from "next/image";
import { useOrchestrator } from "@/app/run/OrchestratorContext";
import RunBar from "./RunBar";
import IntegrationTitle from "./IntegrationTitle";
import FolderPicker from "./FolderPicker";
import SaveButton from "./SaveButton";
import IntegrationsButton from "./IntegrationsButton";
import IntegrationLoader from "./IntegrationLoader";

/**
 * The editor's top bar. The integration controls (title, folder, Save, manage)
 * only appear when an orchestrator is configured (`useOrchestrator().available`);
 * otherwise the bar is just the logo and the RUN control, exactly as before.
 */
export default function EditorHeader() {
  const { available } = useOrchestrator();

  return (
    <header className="flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
      {/* h-6 w-auto controls both axes so Tailwind's `img { height: auto }`
          reset doesn't trigger Next's aspect-ratio warning. */}
      <Image
        src="/octo-logo.png"
        alt="Octo logo"
        width={24}
        height={24}
        className="h-6 w-auto"
        priority
      />
      <span className="font-semibold tracking-tight">Octo</span>

      {available && (
        <>
          <IntegrationLoader />
          <span className="mx-1 h-5 w-px bg-black/10 dark:bg-white/10" />
          <IntegrationTitle />
          <FolderPicker />
          <div className="ml-auto flex items-center gap-2">
            <IntegrationsButton />
            <SaveButton />
            <RunBar />
          </div>
        </>
      )}

      {!available && <RunBar />}
    </header>
  );
}
