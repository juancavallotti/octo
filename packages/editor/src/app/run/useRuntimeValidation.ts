import { useEffect, useMemo, useState } from "react";
import type { EditorDocument } from "../model/document";
import { serializeProject } from "../model/project";
import type { Issue, ValidationResult } from "../model/validate";
import { useEditorState } from "../state/editorState";
import { useProjectFiles } from "../state/useProject";
import { blockIdAddresses } from "./address";
import type { RunTarget, RunTransport, RuntimeIssue } from "./transport";

/** Quiet time after an edit before the project is sent to the runtime to check. */
const VALIDATE_DEBOUNCE_MS = 800;

/**
 * The editor's own validation, joined by what the runtime finds when the transport can
 * ask it (`octo validate`). The runtime is asked only once the editor's checks pass:
 * it would report the same dangling reference again in its own words, and what it adds
 * is what the editor cannot see — a type the binary does not register, a binding the
 * merge resolves differently.
 *
 * Its answer counts only for the files it was given. An edit made since makes it stale,
 * and a stale answer is dropped rather than shown, so it can never hold a run back over
 * a problem already fixed.
 */
export function useRuntimeValidation(
  doc: EditorDocument,
  validation: ValidationResult,
  transport: RunTransport,
  target: RunTarget,
): ValidationResult {
  const { state } = useEditorState();
  const files = useProjectFiles();
  const stored = useMemo(() => serializeProject(files, state.project.name), [files, state.project.name]);
  const signature = useMemo(() => JSON.stringify(stored), [stored]);
  const [checked, setChecked] = useState<{ signature: string; found: RuntimeIssue[] } | null>(null);

  useEffect(() => {
    if (!transport.validate || !validation.ok) return;
    const t = setTimeout(() => {
      transport
        .validate?.({ ...target, files: stored })
        .then((found) => setChecked({ signature, found }))
        .catch(() => {});
    }, VALIDATE_DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [transport, validation.ok, target, stored, signature]);

  return useMemo(() => {
    if (!checked || checked.signature !== signature || checked.found.length === 0) return validation;
    return { ok: false, issues: [...validation.issues, ...toIssues(checked.found, doc)] };
  }, [checked, signature, validation, doc]);
}

/** Runtime issues as editor issues: the block they name, when the address resolves. */
function toIssues(found: RuntimeIssue[], doc: EditorDocument): Issue[] {
  const byAddress = new Map([...blockIdAddresses(doc)].map(([id, address]) => [address, id]));
  return found.map((f) => ({
    message: f.where ? `${f.where}: ${f.message}` : f.message,
    severity: "error",
    blockId: f.where ? byAddress.get(f.where) : undefined,
  }));
}
