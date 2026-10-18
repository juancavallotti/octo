import type { MenuItemConstructorOptions } from "electron";

/**
 * The application menu as data: what it shows follows from the inputs alone, so it is
 * tested without Electron and cannot drift from the state it describes. menu.ts
 * gathers the inputs and installs the result.
 */

/** A command the editor page carries out; its own names (useEditorCommands). */
export type EditorCommand =
  | "undo"
  | "redo"
  | "save"
  | "run"
  | "run-flow"
  | "stop"
  | "view:flows"
  | "view:resources"
  | "view:testing"
  | "flows:canvas"
  | "flows:yaml"
  | "toggle:components"
  | "toggle:settings"
  | "toggle:console"
  | "canvas:zoom-in"
  | "canvas:zoom-out"
  | "canvas:zoom-reset"
  | "new:flow"
  | "new:file"
  | "new:resource"
  | "new:suite";

/**
 * What the editor page reports about itself, so the menu can show it: which commands
 * are possible now, and which view and panels are showing. Declared here rather than
 * imported, as the preload's bridge is: neither side may depend on the other.
 */
export interface EditorStatus {
  view: "flows" | "resources" | "testing";
  flowsView: "canvas" | "yaml";
  hasResources: boolean;
  hasTests: boolean;
  editingText: boolean;
  canUndo: boolean;
  canRedo: boolean;
  canSave: boolean;
  runs: "integration" | "tests";
  canRun: boolean;
  canRunFlow: boolean;
  canStop: boolean;
  canvas: boolean;
  panels: { components: boolean; settings: boolean; console: boolean } | null;
}

export interface MenuInput {
  isMac: boolean;
  /** The application's name, the macOS app menu's title. */
  appName: string;
  /**
   * The editor's last report, or null before the page has made one (still loading, or
   * a page too old to report). Without one, editor items are disabled and Undo and Redo
   * fall back to the platform's, so typing still undoes.
   */
  status: EditorStatus | null;
  recents: { label: string; path: string }[];
  /** A folder is open, so it can be revealed. */
  vaultOpen: boolean;
  /** The editor server is up, so it has an MCP endpoint to copy. */
  mcpAvailable: boolean;
  /** The update item: a check, or the restart a downloaded version is waiting for. */
  update: { label: string };
  /** Whether docker can build and run the folder's container right now. */
  docker: "missing" | "stopped" | "running";
}

export interface MenuActions {
  command(command: EditorCommand): void;
  /** Zoom the page itself, for when the canvas is not what is showing. */
  pageZoom(direction: "in" | "out" | "reset"): void;
  pickVault(): void;
  switchVault(path: string): void;
  revealVault(): void;
  copyMcpUrl(): void;
  openSettings(): void;
  /** Open the Container window at the part an item names. */
  openContainer(focus: "build" | "push" | "export"): void;
  update(): void;
  openDocs(): void;
  showLog(): void;
}

export function menuTemplate(input: MenuInput, act: MenuActions): MenuItemConstructorOptions[] {
  const { isMac, status } = input;
  const editor = status !== null;
  const command = (c: EditorCommand) => () => act.command(c);

  const appItems: MenuItemConstructorOptions[] = [
    {
      label: "Copy MCP Endpoint URL",
      // The one thing an agent needs to attach to this editor.
      enabled: input.mcpAvailable,
      click: act.copyMcpUrl,
    },
    { label: input.update.label, click: act.update },
    { type: "separator" },
    { label: "Settings…", accelerator: "CmdOrCtrl+,", click: act.openSettings },
  ];

  const newItems: MenuItemConstructorOptions[] = [
    { label: "Flow", accelerator: "CmdOrCtrl+N", enabled: editor, click: command("new:flow") },
    {
      label: "Config File…",
      accelerator: "CmdOrCtrl+Shift+N",
      enabled: editor,
      click: command("new:file"),
    },
    { type: "separator" },
    {
      label: "Resource…",
      enabled: editor && status.hasResources,
      click: command("new:resource"),
    },
    { label: "Test Suite", enabled: editor && status.hasTests, click: command("new:suite") },
  ];

  const recents: MenuItemConstructorOptions[] =
    input.recents.length === 0
      ? [{ label: "No Recent Folders", enabled: false }]
      : input.recents.map((r) => ({
          label: r.label,
          // The full path in the tooltip: two folders called "flows" are not unusual.
          toolTip: r.path,
          click: () => act.switchVault(r.path),
        }));

  const file: MenuItemConstructorOptions = {
    label: "File",
    submenu: [
      { label: "New", submenu: newItems },
      { type: "separator" },
      {
        label: "Save",
        accelerator: "CmdOrCtrl+S",
        enabled: editor && status.canSave,
        click: command("save"),
      },
      { type: "separator" },
      { label: "Open Folder…", accelerator: "CmdOrCtrl+Shift+O", click: act.pickVault },
      { label: "Open Recent", submenu: recents },
      { type: "separator" },
      {
        label: isMac ? "Reveal Folder in Finder" : "Show Folder in File Manager",
        enabled: input.vaultOpen,
        click: act.revealVault,
      },
      ...(isMac
        ? []
        : ([{ type: "separator" }, ...appItems, { type: "separator" }, { role: "quit" }] satisfies
            MenuItemConstructorOptions[])),
    ],
  };

  // Undo and Redo are the editor's while it reports: in a text field they step the
  // field, elsewhere the document. Before it does, the platform's own keep typing
  // undoable.
  const history: MenuItemConstructorOptions[] = editor
    ? [
        {
          label: "Undo",
          accelerator: "CmdOrCtrl+Z",
          enabled: status.editingText || status.canUndo,
          click: command("undo"),
        },
        {
          label: "Redo",
          accelerator: "CmdOrCtrl+Shift+Z",
          enabled: status.editingText || status.canRedo,
          click: command("redo"),
        },
      ]
    : [{ role: "undo" }, { role: "redo" }];

  const edit: MenuItemConstructorOptions = {
    label: "Edit",
    submenu: [
      ...history,
      { type: "separator" },
      // The platform's own: they act on whatever has focus, a field or the page.
      { role: "cut" },
      { role: "copy" },
      { role: "paste" },
      ...(isMac ? ([{ role: "pasteAndMatchStyle" }] satisfies MenuItemConstructorOptions[]) : []),
      { role: "delete" },
      { role: "selectAll" },
    ],
  };

  // Zoom means the canvas while it is showing — the canvas binds the same keys — and
  // the page everywhere else.
  const canvas = editor && status.canvas;
  const zoom = (c: EditorCommand, page: "in" | "out" | "reset") => () =>
    canvas ? act.command(c) : act.pageZoom(page);

  const view: MenuItemConstructorOptions = {
    label: "View",
    submenu: [
      {
        label: "Flows",
        type: "radio",
        accelerator: "CmdOrCtrl+1",
        enabled: editor,
        checked: editor && status.view === "flows",
        click: command("view:flows"),
      },
      ...(editor && status.hasResources
        ? ([
            {
              label: "Resources",
              type: "radio",
              accelerator: "CmdOrCtrl+2",
              checked: status.view === "resources",
              click: command("view:resources"),
            },
          ] satisfies MenuItemConstructorOptions[])
        : []),
      ...(editor && status.hasTests
        ? ([
            {
              label: "Testing",
              type: "radio",
              accelerator: "CmdOrCtrl+3",
              checked: status.view === "testing",
              click: command("view:testing"),
            },
          ] satisfies MenuItemConstructorOptions[])
        : []),
      { type: "separator" },
      {
        label: "Canvas",
        type: "radio",
        enabled: editor,
        checked: editor && status.flowsView === "canvas",
        click: command("flows:canvas"),
      },
      {
        label: "YAML",
        type: "radio",
        enabled: editor,
        checked: editor && status.flowsView === "yaml",
        click: command("flows:yaml"),
      },
      { type: "separator" },
      ...(editor && status.panels
        ? ([
            {
              label: "Show Components",
              type: "checkbox",
              enabled: status.canvas,
              checked: status.panels.components,
              click: command("toggle:components"),
            },
            {
              label: "Show Settings",
              type: "checkbox",
              enabled: status.canvas,
              checked: status.panels.settings,
              click: command("toggle:settings"),
            },
            {
              label: "Show Console",
              type: "checkbox",
              checked: status.panels.console,
              click: command("toggle:console"),
            },
            { type: "separator" },
          ] satisfies MenuItemConstructorOptions[])
        : []),
      {
        label: canvas ? "Zoom Canvas In" : "Zoom In",
        accelerator: "CmdOrCtrl+=",
        click: zoom("canvas:zoom-in", "in"),
      },
      {
        label: canvas ? "Zoom Canvas Out" : "Zoom Out",
        accelerator: "CmdOrCtrl+-",
        click: zoom("canvas:zoom-out", "out"),
      },
      {
        label: canvas ? "Canvas Actual Size" : "Actual Size",
        accelerator: "CmdOrCtrl+0",
        click: zoom("canvas:zoom-reset", "reset"),
      },
      { type: "separator" },
      { role: "reload" },
      { role: "toggleDevTools" },
      { type: "separator" },
      { role: "togglefullscreen" },
    ],
  };

  const tests = editor && status.runs === "tests";
  const run: MenuItemConstructorOptions = {
    label: "Run",
    submenu: [
      {
        label: tests ? "Run All Tests" : "Run Integration",
        accelerator: "CmdOrCtrl+Enter",
        enabled: editor && status.canRun,
        click: command("run"),
      },
      {
        label: "Run Current Flow",
        accelerator: "CmdOrCtrl+Shift+Enter",
        enabled: editor && status.canRunFlow,
        click: command("run-flow"),
      },
      { type: "separator" },
      {
        label: "Stop",
        accelerator: "CmdOrCtrl+.",
        enabled: editor && status.canStop,
        click: command("stop"),
      },
    ],
  };

  // Exporting a Dockerfile only writes files, so it is there with or without docker;
  // building, running and pushing need its daemon, and the first item says why not.
  const docker = input.docker === "running";
  const container: MenuItemConstructorOptions = {
    label: "Container",
    submenu: [
      ...(docker
        ? []
        : ([
            {
              label: input.docker === "stopped" ? "Docker Is Not Running" : "Docker Is Not Installed",
              enabled: false,
            },
            { type: "separator" },
          ] satisfies MenuItemConstructorOptions[])),
      {
        label: "Build and Run…",
        enabled: docker && input.vaultOpen,
        click: () => act.openContainer("build"),
      },
      {
        label: "Push Image…",
        enabled: docker && input.vaultOpen,
        click: () => act.openContainer("push"),
      },
      { type: "separator" },
      {
        label: "Export Dockerfile…",
        enabled: input.vaultOpen,
        click: () => act.openContainer("export"),
      },
    ],
  };

  return [
    ...(isMac
      ? ([
          {
            label: input.appName,
            submenu: [
              { role: "about" },
              { type: "separator" },
              ...appItems,
              { type: "separator" },
              { role: "hide" },
              { role: "hideOthers" },
              { type: "separator" },
              { role: "quit" },
            ],
          },
        ] satisfies MenuItemConstructorOptions[])
      : []),
    file,
    edit,
    view,
    run,
    container,
    { role: "windowMenu" },
    {
      role: "help",
      submenu: [
        { label: "Octo Documentation", click: act.openDocs },
        { label: "Show Server Log", click: act.showLog },
      ],
    },
  ];
}
