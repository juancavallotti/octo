"use client";

import { useEffect, useMemo, useState } from "react";
import { FileText, FolderClosed } from "lucide-react";
import { useEditorState } from "../../state/editorState";
import { useResourceStore } from "../../providers/ResourceStoreProvider";
import type { StoredResource } from "../../providers/ResourceStoreProvider";
import { reconcileResources, type ReconciledResource } from "./reconcile";

/**
 * The Resources tab: a file browser (left) and content view (right) over the
 * integration's resources. It reconciles what the store holds with the document's
 * declared `resources:` — tracked files are editable, files that exist but aren't
 * declared are shown gray + italic and never mutated (see reconcile.ts). This
 * commit is read-only browse; editing and file management land in later commits.
 */
export default function ResourcesView() {
  const { state } = useEditorState();
  const store = useResourceStore();
  const integrationId = state.integration.id;

  const [stored, setStored] = useState<StoredResource[]>([]);
  const [status, setStatus] = useState<"loading" | "ready" | "error">(
    "loading",
  );
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);

  // (Re)load whenever the store or the saved integration id changes — the id
  // flips from null to a value on the first save, which should populate the tab.
  useEffect(() => {
    if (!store) return;
    let cancelled = false;
    setStatus("loading");
    store
      .list()
      .then((rows) => {
        if (cancelled) return;
        setStored(rows);
        setStatus("ready");
      })
      .catch((e) => {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
        setStatus("error");
      });
    return () => {
      cancelled = true;
    };
  }, [store, integrationId]);

  const entries = useMemo(
    () => reconcileResources(stored, state.document.resources),
    [stored, state.document.resources],
  );

  const current = entries.find((e) => e.name === selected) ?? null;

  if (!integrationId) {
    return (
      <Centered>
        Save the integration to manage its resources.
      </Centered>
    );
  }
  if (status === "loading") {
    return <Centered>Loading resources…</Centered>;
  }
  if (status === "error") {
    return <Centered>Couldn’t load resources: {error}</Centered>;
  }

  return (
    <div className="flex flex-1 min-h-0">
      <aside className="w-64 shrink-0 overflow-y-auto border-r border-black/10 dark:border-white/10">
        {entries.length === 0 ? (
          <p className="px-3 py-4 text-xs text-zinc-400 dark:text-zinc-500">
            No resources yet.
          </p>
        ) : (
          <ul className="py-1">
            {entries.map((entry) => (
              <ResourceRow
                key={entry.name}
                entry={entry}
                active={entry.name === selected}
                onSelect={() => setSelected(entry.name)}
              />
            ))}
          </ul>
        )}
      </aside>
      <section className="flex flex-1 min-h-0 flex-col overflow-hidden">
        <ContentPane entry={current} />
      </section>
    </div>
  );
}

function ResourceRow({
  entry,
  active,
  onSelect,
}: {
  entry: ReconciledResource;
  active: boolean;
  onSelect: () => void;
}) {
  const outOfScope = entry.scope === "out-of-scope";
  const missing = entry.scope === "declared-missing";
  return (
    <li>
      <button
        type="button"
        onClick={onSelect}
        title={outOfScope ? "Not in project (declared nowhere)" : entry.name}
        className={`flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm transition-colors ${
          active
            ? "bg-black/[0.06] dark:bg-white/10"
            : "hover:bg-black/[0.04] dark:hover:bg-white/[0.06]"
        } ${
          outOfScope
            ? "italic text-zinc-400 dark:text-zinc-500"
            : missing
              ? "text-amber-600 dark:text-amber-400"
              : "text-zinc-700 dark:text-zinc-200"
        }`}
      >
        <FileText size={14} className="shrink-0 opacity-70" />
        <span className="truncate">{entry.name}</span>
      </button>
    </li>
  );
}

function ContentPane({ entry }: { entry: ReconciledResource | null }) {
  if (!entry) {
    return (
      <Centered>
        <FolderClosed size={20} className="mb-2 opacity-50" />
        Select a resource to view its contents.
      </Centered>
    );
  }

  return (
    <>
      <header className="flex items-center gap-2 border-b border-black/10 px-3 py-2 dark:border-white/10">
        <span className="font-mono text-sm">{entry.name}</span>
        <ScopeBadge entry={entry} />
      </header>
      {entry.scope === "declared-missing" ? (
        <Centered>
          Declared in the config but no content stored yet.
        </Centered>
      ) : (
        <pre className="flex-1 overflow-auto whitespace-pre-wrap break-words p-3 font-mono text-xs text-zinc-800 dark:text-zinc-200">
          {entry.stored?.content ?? ""}
        </pre>
      )}
    </>
  );
}

function ScopeBadge({ entry }: { entry: ReconciledResource }) {
  if (entry.scope === "out-of-scope") {
    return (
      <span className="rounded-full bg-black/[0.06] px-1.5 py-0.5 text-[10px] italic text-zinc-500 dark:bg-white/10">
        not in project
      </span>
    );
  }
  if (entry.scope === "declared-missing") {
    return (
      <span className="rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-600 dark:text-amber-400">
        missing
      </span>
    );
  }
  return (
    <span className="rounded-full bg-black/[0.06] px-1.5 py-0.5 text-[10px] text-zinc-500 dark:bg-white/10">
      {entry.kind}
    </span>
  );
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center p-6 text-center text-sm text-zinc-400 dark:text-zinc-500">
      {children}
    </div>
  );
}
