"use client";

import { useRef } from "react";
import { fromDefinitionYaml } from "@octo/editor";
import {
  createIntegration,
  deleteIntegration,
  updateIntegration,
  type Integration,
} from "@/app/model/orchestrator";
import { nameFromFilename } from "./yamlFile";

/**
 * The four things you can do to an integration itself: import one from a file,
 * duplicate the selected one, rename it, delete it.
 *
 * Separate from the folder tree's own mutations because these all turn on
 * `selected` and none of them touches the tree's shape. `renameSelected` is the
 * odd one out and returns a boolean rather than going through `run`: the inline
 * editor has to stay open on a name conflict rather than close as though the
 * rename had worked.
 */
export function useIntegrationActions({
  selected,
  run,
  refresh,
  confirm,
  selectIntegration,
  setBusy,
  setError,
}: {
  selected: Integration | null;
  run: (fn: () => Promise<unknown>) => Promise<void>;
  refresh: () => Promise<unknown>;
  confirm: (opts: {
    title: string;
    body?: string;
    confirmLabel?: string;
    danger?: boolean;
  }) => Promise<boolean>;
  selectIntegration: (id: string | null) => void;
  setBusy: (busy: boolean) => void;
  setError: (message: string | null) => void;
}) {
  // Hidden file input backing the "Import" button. Importing a .yaml always
  // creates a new integration (name from the filename); the file's contents are
  // the runtime definition, validated before the create so malformed YAML fails
  // fast with an inline error instead of a broken record.
  const importInput = useRef<HTMLInputElement>(null);

  const onImportFile = async (file: File) => {
    setError(null);
    const text = await file.text();
    try {
      fromDefinitionYaml(text);
    } catch (e) {
      setError(`Invalid integration YAML: ${(e as Error).message}`);
      return;
    }
    run(async () => {
      const created = await createIntegration({
        name: nameFromFilename(file.name),
        definition: text,
      });
      selectIntegration(created.id);
    });
  };

  // Duplicate the selected integration into a fresh "Copy of …" record, then
  // select the copy. Its definition is already loaded in the list, so no fetch.
  const copySelected = () => {
    if (!selected) return;
    run(async () => {
      const created = await createIntegration({
        name: `Copy of ${selected.name}`,
        definition: selected.definition,
      });
      selectIntegration(created.id);
    });
  };

  // Rename the selected integration (its name is effectively its filename),
  // preserving the definition. The updated name lands via the refresh. Returns
  // whether the rename succeeded so the inline editor can stay open on conflict
  // (e.g. a duplicate name); failures still surface in the inline error banner.
  const renameSelected = async (name: string): Promise<boolean> => {
    if (!selected) return false;
    setBusy(true);
    setError(null);
    try {
      await updateIntegration(selected.id, {
        name,
        definition: selected.definition,
      });
      await refresh();
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    } finally {
      setBusy(false);
    }
  };

  const removeSelected = async () => {
    if (!selected) return;
    const ok = await confirm({
      title: `Delete integration "${selected.name}"?`,
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    const id = selected.id;
    selectIntegration(null);
    run(() => deleteIntegration(id));
  };

  return {
    importInput,
    onImportFile,
    copySelected,
    renameSelected,
    removeSelected,
  };
}
