import { describe, expect, it, vi } from "vitest";
import type { MenuItemConstructorOptions } from "electron";
import { menuTemplate, type EditorStatus, type MenuActions, type MenuInput } from "./menuTemplate";

const status: EditorStatus = {
  view: "flows",
  flowsView: "canvas",
  hasResources: true,
  hasTests: true,
  hasTerminal: true,
  editingText: false,
  canUndo: true,
  canRedo: false,
  canSave: true,
  runs: "integration",
  canRun: true,
  canRunFlow: false,
  canStop: false,
  canvas: true,
  panels: { components: true, settings: false, console: false },
};

function actions(): MenuActions {
  return {
    command: vi.fn(),
    pageZoom: vi.fn(),
    pickVault: vi.fn(),
    switchVault: vi.fn(),
    revealVault: vi.fn(),
    copyMcpUrl: vi.fn(),
    openSettings: vi.fn(),
    openContainer: vi.fn(),
    openPlatform: vi.fn(),
    pushToPlatform: vi.fn(),
    useInstallation: vi.fn(),
    signIn: vi.fn(),
    signOut: vi.fn(),
    update: vi.fn(),
    openDocs: vi.fn(),
    showLog: vi.fn(),
  };
}

function build(over: Partial<MenuInput> = {}, act = actions()) {
  const input: MenuInput = {
    isMac: true,
    appName: "Octo",
    status,
    recents: [],
    vaultOpen: true,
    mcpAvailable: true,
    update: { label: "Check for Updates…" },
    docker: "running",
    linked: null,
    installations: [],
    ...over,
  };
  return { menu: menuTemplate(input, act), act };
}

/** The item at a path of labels (or roles), e.g. ["File", "New", "Flow"]. */
function item(menu: MenuItemConstructorOptions[], ...path: string[]): MenuItemConstructorOptions {
  let items = menu;
  let found: MenuItemConstructorOptions | undefined;
  for (const name of path) {
    found = items.find((i) => i.label === name || i.role === name);
    if (!found) throw new Error(`no menu item ${path.join(" › ")}`);
    items = (found.submenu as MenuItemConstructorOptions[] | undefined) ?? [];
  }
  return found!;
}

const click = (i: MenuItemConstructorOptions) =>
  (i.click as unknown as () => void)();

describe("menuTemplate", () => {
  it("has the File, Edit, View and Run menus after the app menu", () => {
    const { menu } = build();
    expect(menu.map((m) => m.label ?? m.role)).toEqual([
      "Octo",
      "File",
      "Edit",
      "View",
      "Run",
      "Container",
      "Platform",
      "windowMenu",
      "help",
    ]);
  });

  it("enables what the editor says it can do, and nothing else", () => {
    const { menu } = build();
    expect(item(menu, "Edit", "Undo").enabled).toBe(true);
    expect(item(menu, "Edit", "Redo").enabled).toBe(false);
    expect(item(menu, "File", "Save").enabled).toBe(true);
    expect(item(menu, "Run", "Run Integration").enabled).toBe(true);
    expect(item(menu, "Run", "Run Current Flow").enabled).toBe(false);
    expect(item(menu, "Run", "Stop").enabled).toBe(false);
  });

  // In a text field, Undo steps the field, which always has something to offer.
  it("keeps Undo and Redo enabled while text is being edited", () => {
    const { menu } = build({ status: { ...status, editingText: true, canUndo: false } });
    expect(item(menu, "Edit", "Undo").enabled).toBe(true);
    expect(item(menu, "Edit", "Redo").enabled).toBe(true);
  });

  it("forwards a menu item to the editor as its command", () => {
    const { menu, act } = build();
    click(item(menu, "File", "New", "Test Suite"));
    click(item(menu, "View", "Testing"));
    click(item(menu, "Edit", "Undo"));
    expect(act.command).toHaveBeenNthCalledWith(1, "new:suite");
    expect(act.command).toHaveBeenNthCalledWith(2, "view:testing");
    expect(act.command).toHaveBeenNthCalledWith(3, "undo");
  });

  it("checks the view and panels that are showing", () => {
    const { menu } = build();
    expect(item(menu, "View", "Flows").checked).toBe(true);
    expect(item(menu, "View", "Testing").checked).toBe(false);
    expect(item(menu, "View", "Canvas").checked).toBe(true);
    expect(item(menu, "View", "Show Components").checked).toBe(true);
    expect(item(menu, "View", "Show Settings").checked).toBe(false);
  });

  it("names the Testing tab's run for what it runs", () => {
    const { menu } = build({ status: { ...status, view: "testing", runs: "tests", canvas: false } });
    expect(item(menu, "Run", "Run All Tests").accelerator).toBe("CmdOrCtrl+Enter");
  });

  it("offers only the tabs the host backs", () => {
    const { menu } = build({ status: { ...status, hasResources: false } });
    expect(() => item(menu, "View", "Resources")).toThrow();
    expect(item(menu, "File", "New", "Resource…").enabled).toBe(false);
  });

  it("offers the terminal on VS Code's keys when the editor has one", () => {
    const { menu, act } = build();
    expect(item(menu, "View", "Terminal").accelerator).toBe("Ctrl+`");
    click(item(menu, "View", "Terminal"));
    click(item(menu, "View", "New Terminal"));
    expect(act.command).toHaveBeenNthCalledWith(1, "terminal:toggle");
    expect(act.command).toHaveBeenNthCalledWith(2, "terminal:new");

    const without = build({ status: { ...status, hasTerminal: false } });
    expect(() => item(without.menu, "View", "Terminal")).toThrow();
  });

  // The canvas binds the zoom keys while it shows; elsewhere they zoom the page.
  it("zooms the canvas when it shows, and the page otherwise", () => {
    const onCanvas = build();
    click(item(onCanvas.menu, "View", "Zoom Canvas In"));
    expect(onCanvas.act.command).toHaveBeenCalledWith("canvas:zoom-in");

    const elsewhere = build({ status: { ...status, flowsView: "yaml", canvas: false } });
    click(item(elsewhere.menu, "View", "Zoom In"));
    expect(elsewhere.act.pageZoom).toHaveBeenCalledWith("in");
    expect(elsewhere.act.command).not.toHaveBeenCalled();
  });

  // Before the page reports, typing must still undo: the platform's roles stand in.
  it("falls back to the platform's undo and disables editor items before a report", () => {
    const { menu } = build({ status: null });
    expect(item(menu, "Edit", "undo").role).toBe("undo");
    expect(item(menu, "File", "Save").enabled).toBe(false);
    expect(item(menu, "File", "New", "Flow").enabled).toBe(false);
    expect(item(menu, "Run", "Run Integration").enabled).toBe(false);
  });

  it("gates building, running and pushing on docker, but never the Dockerfile export", () => {
    const up = build();
    expect(item(up.menu, "Container", "Build and Run…").enabled).toBe(true);
    expect(item(up.menu, "Container", "Push Image…").enabled).toBe(true);

    const stopped = build({ docker: "stopped" });
    expect(item(stopped.menu, "Container", "Docker Is Not Running").enabled).toBe(false);
    expect(item(stopped.menu, "Container", "Build and Run…").enabled).toBe(false);
    expect(item(stopped.menu, "Container", "Push Image…").enabled).toBe(false);
    expect(item(stopped.menu, "Container", "Export Dockerfile…").enabled).toBe(true);

    const missing = build({ docker: "missing" });
    expect(() => item(missing.menu, "Container", "Docker Is Not Installed")).not.toThrow();
  });

  it("opens the Container window at the part an item names", () => {
    const { menu, act } = build({ docker: "stopped" });
    click(item(menu, "Container", "Export Dockerfile…"));
    expect(act.openContainer).toHaveBeenCalledWith("export");
  });

  it("acts on the folder's integration only when the folder is linked to one", () => {
    const unlinked = build();
    for (const label of ["Push to Platform", "Versions…", "Deploy…", "Logs…"]) {
      expect(item(unlinked.menu, "Platform", label).enabled).toBe(false);
    }
    expect(item(unlinked.menu, "Platform", "Put This Folder on the Platform…").enabled).toBe(true);
    expect(item(unlinked.menu, "Platform", "Pull Integration…").enabled).not.toBe(false);

    const { menu, act } = build({ linked: { integration: "billing", profile: "prod" } });
    expect(item(menu, "Platform", "billing on prod").enabled).toBe(false);
    click(item(menu, "Platform", "Push to Platform"));
    expect(act.pushToPlatform).toHaveBeenCalled();
    click(item(menu, "Platform", "Deploy…"));
    expect(act.openPlatform).toHaveBeenCalledWith("deployments");
  });

  it("switches installations, and offers adding one when there is none", () => {
    expect(item(build().menu, "Platform", "Installation", "Add Installation…").enabled).not.toBe(false);

    const { menu, act } = build({
      installations: [
        { name: "dev", current: true, identity: { state: "signed-out" } },
        { name: "prod", current: false, identity: { state: "checking" } },
      ],
    });
    expect(item(menu, "Platform", "Installation", "dev").checked).toBe(true);
    click(item(menu, "Platform", "Installation", "dev"));
    expect(act.useInstallation).not.toHaveBeenCalled();
    click(item(menu, "Platform", "Installation", "prod"));
    expect(act.useInstallation).toHaveBeenCalledWith("prod");
    click(item(menu, "Platform", "Installation", "Manage Installations…"));
    expect(act.openPlatform).toHaveBeenCalledWith("settings");
  });

  it("signs in to and out of the current installation", () => {
    const signedOut = build({ installations: [{ name: "dev", current: true, identity: { state: "signed-out" } }] });
    click(item(signedOut.menu, "Platform", "Sign In to dev…"));
    expect(signedOut.act.signIn).toHaveBeenCalledWith("dev");

    const checking = build({ installations: [{ name: "dev", current: true, identity: { state: "checking" } }] });
    expect(item(checking.menu, "Platform", "Checking dev…").enabled).toBe(false);

    const oauth = build({
      installations: [{ name: "dev", current: true, identity: { state: "signed-in", credential: "oauth", name: "Juan" } }],
    });
    expect(item(oauth.menu, "Platform", "Signed in to dev as Juan").enabled).toBe(false);
    click(item(oauth.menu, "Platform", "Sign Out of dev"));
    expect(oauth.act.signOut).toHaveBeenCalledWith("dev");

    // octopaas's logout forgets a session, never a key, so a key has no Sign Out.
    const key = build({
      installations: [{ name: "dev", current: true, identity: { state: "signed-in", credential: "apiKey", name: "ci" } }],
    });
    expect(item(key.menu, "Platform", "Using an API key on dev").enabled).toBe(false);
    expect(() => item(key.menu, "Platform", "Sign Out of dev")).toThrow();
  });

  it("puts the app items under File off macOS", () => {
    const { menu } = build({ isMac: false });
    expect(menu[0].label).toBe("File");
    expect(item(menu, "File", "Settings…").accelerator).toBe("CmdOrCtrl+,");
  });
});
