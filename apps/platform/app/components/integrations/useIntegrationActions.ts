"use client";

import { useRef } from "react";
import { fromDefinitionYaml } from "@octo/editor";
import {
  createIntegration,
  deleteIntegration,
  exportBundle,
  exportSnapshotBundle,
  importBundle,
  replaceBundle,
  updateIntegration,
  type Integration,
} from "@/app/model/orchestrator";
import {
  downloadBundle,
  isBundleFile,
  nameFromFilename,
  readFileBytes,
} from "./files";

/**
 * The things you can do to an integration itself: import one from a file,
 * download it as a bundle, replace its contents from one, duplicate the selected
 * one, rename it, delete it.
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
  // Hidden file input backing the "Import" button, and the hidden one backing
  // "Replace from bundle". Importing either shape — a bare .yaml definition or a
  // .zip bundle — always creates a new integration.
  const importInput = useRef<HTMLInputElement>(null);
  const replaceInput = useRef<HTMLInputElement>(null);

  const onImportFile = async (file: File) => {
    setError(null);
    if (isBundleFile(file)) {
      // A bundle is read by the orchestrator, which is the only thing that knows
      // the archive format; the filename only names an archive whose manifest
      // doesn't. Everything else — validity, naming conflicts — comes back as an
      // error result and lands in the inline banner.
      run(async () => {
        const created = await importBundle(
          await readFileBytes(file),
          nameFromFilename(file.name),
        );
        selectIntegration(created.id);
      });
      return;
    }
    // A .yaml is the definition itself, validated here before the create so
    // malformed YAML fails fast with an inline error instead of a broken record.
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

  // Download the selected integration and every resource it owns as one archive.
  // `snapshot` is the active version: a tag exports its frozen contents, and null
  // (Current) exports the working copy.
  const downloadSelectedBundle = (
    snapshot: { id: string; tag: string } | null,
  ) => {
    if (!selected) return;
    run(async () => {
      const archive = snapshot
        ? await exportSnapshotBundle(snapshot.id)
        : await exportBundle(selected.id);
      downloadBundle(selected.name, archive, snapshot?.tag);
    });
  };

  // Overwrite the selected integration's definition and resource set from a
  // bundle. Destructive to the working copy — resources the bundle doesn't carry
  // are deleted — so it confirms first, naming what stays behind.
  const replaceSelectedFromBundle = async (file: File) => {
    if (!selected) return;
    setError(null);
    const ok = await confirm({
      title: `Replace "${selected.name}" from ${file.name}?`,
      body:
        "Its definition and resources are overwritten with the bundle's, and " +
        "resources the bundle doesn't carry are deleted. Version tags and " +
        "anything already deployed are untouched.",
      confirmLabel: "Replace",
      danger: true,
    });
    if (!ok) return;
    run(async () => {
      await replaceBundle(selected.id, await readFileBytes(file));
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
    replaceInput,
    onImportFile,
    downloadSelectedBundle,
    replaceSelectedFromBundle,
    copySelected,
    renameSelected,
    removeSelected,
  };
}
