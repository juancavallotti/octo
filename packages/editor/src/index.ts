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
export {
  planBreakpoint,
  naturalAddress,
  blockIdAddresses,
  namesNeededFor,
  debugConflict,
  type BreakpointPlan,
  type BlockRename,
} from "./app/run/address";
export {
  useFlowRun,
  type FlowRunEntry,
  type FlowRunStatus,
} from "./app/run/FlowRunContext";
export { useConsole, type ConsoleTab } from "./app/run/console";
export type {
  RunTransport,
  RunStatusSnapshot,
  CelEvalRequest,
  CelEvalResult,
  FlowRunRequest,
  FlowRunOutcome,
  BreakOutcome,
} from "./app/run/transport";
export type { DevEnvStore } from "./app/state/devEnvStore";
export {
  EditorMetaProvider,
  useEditorMeta,
  EDITOR_META_RESOURCE,
  type EditorMetaStore,
} from "./app/providers/EditorMetaProvider";
export type { TestInput, EditorMeta, FlowMeta } from "./app/meta/types";

// Resource-store capability (backs the Resources tab)
export {
  ResourceStoreProvider,
  useResourceStore,
  type ResourceStore,
  type StoredResource,
  type CreateResourceInput,
  type UpdateResourcePatch,
} from "./app/providers/ResourceStoreProvider";

// Document model + serialization (handy for apps building loaders/savers)
export {
  toRunnableYaml,
  toDefinitionYaml,
  fromDefinitionYaml,
} from "./app/model/runConfig";
export {
  validateDocument,
  issueMessages,
  type ValidationResult,
  type Issue,
} from "./app/model/validate";
export * from "./app/model/document";

// Capability schema helpers (icon registry + connector/source specs) are exposed
// on the server-safe `@octo/editor/runtime` subpath, so a host can badge an
// integration by its source/connector type without importing the React editor.
// The injection seam, though, lives here on the main entry: a host injects the
// runtime-generated schema (falling back to the bundled one) before first render.
export { setCapabilities, getCapabilities } from "./app/schema";
export type { Capabilities } from "./app/schema/types";
