/**
 * Public API of the @octo/editor library: the embeddable visual flow editor plus
 * the capability contracts an embedding app implements (filesystem + run). The
 * app supplies the concrete capabilities; this package owns the editor itself.
 */

// Editor shell + state
export { default as EditorRoot } from "./app/components/EditorRoot";
export {
  EditorStateProvider,
  useEditorState,
  EditorActionType,
  type EditorState,
} from "./app/state/editorState";

// Editor controls (composed by an app's header)
export { default as RunBar } from "./app/components/RunBar";
export { default as SaveButton } from "./app/components/SaveButton";
export { useSave, type SaveController } from "./app/save/SaveContext";
export { default as FolderPicker } from "./app/components/FolderPicker";
export { default as ViewModeToggle } from "./app/components/ViewModeToggle";
export { default as IntegrationTitle } from "./app/components/IntegrationTitle";
export { default as IntegrationLoader } from "./app/components/IntegrationLoader";

// Filesystem capability (load/save)
export {
  FileSystemProvider,
  useFileSystem,
  type FileSystemCapability,
  type StoredDocument,
  type SaveInput,
  type FolderNode,
  type FolderCapability,
} from "./app/providers/FileSystemProvider";

// Run capability
export { RunProvider, useRun, type RunLogLine } from "./app/run/RunContext";
export type { RunTransport, RunStatusSnapshot } from "./app/run/transport";
export type { DevEnvStore } from "./app/state/devEnvStore";

// Document model + serialization (handy for apps building loaders/savers)
export {
  toRunnableYaml,
  toDefinitionYaml,
  fromDefinitionYaml,
} from "./app/model/runConfig";
export { validateDocument, type ValidationResult } from "./app/model/validate";
export * from "./app/model/document";
