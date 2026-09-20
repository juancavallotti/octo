import { Menu, app, clipboard, shell, type MenuItemConstructorOptions } from "electron";
import path from "node:path";
import { logPath } from "./log";
import { current } from "./server";
import { openSettings } from "./settingsWindow";
import { checkForUpdates, restartToUpdate, updateStatus } from "./update";
import { openVault, pickVault, recents, switchTo } from "./vault";

/**
 * The application menu, rebuilt from current state on every change rather than
 * mutated in place, so its dynamic parts cannot drift from what they describe.
 */

function recentsSubmenu(): MenuItemConstructorOptions[] {
  const items = recents();
  if (items.length === 0) {
    return [{ label: "No Recent Folders", enabled: false }];
  }
  return items.map((v) => ({
    label: path.basename(v.path),
    // The full path in the tooltip: two folders called "flows" are not unusual.
    toolTip: v.path,
    click: () => void switchTo(v.path),
  }));
}

/** The MCP endpoint, which is the editor's own URL plus /mcp. */
export function mcpUrl(): string | null {
  const server = current();
  return server ? `${server.url}/mcp` : null;
}

export function copyMcpUrl(): void {
  const url = mcpUrl();
  if (url) clipboard.writeText(url);
}

export function revealVault(): void {
  const vault = openVault();
  if (vault) void shell.openPath(vault.path);
}

/**
 * The update entry: one item that either offers the restart a downloaded update is
 * waiting for, or the check that might find one. Two items would mean one of them is
 * always the wrong thing to click.
 */
function updateItem(): MenuItemConstructorOptions {
  const { stage, version } = updateStatus();
  if (stage === "ready" && version) {
    return { label: `Restart to Update to ${version}`, click: restartToUpdate };
  }
  return { label: "Check for Updates…", click: () => void checkForUpdates(false) };
}

export function buildMenu(): void {
  const isMac = process.platform === "darwin";

  const template: MenuItemConstructorOptions[] = [
    ...(isMac
      ? ([{
          label: app.name,
          submenu: [
            { role: "about" },
            { type: "separator" },
            {
              label: "Copy MCP Endpoint URL",
              // The one thing an agent needs to attach to this editor.
              enabled: mcpUrl() !== null,
              click: copyMcpUrl,
            },
            updateItem(),
            { type: "separator" },
            {
              label: "Settings…",
              accelerator: "CmdOrCtrl+,",
              click: openSettings,
            },
            { type: "separator" },
            { role: "hide" },
            { role: "hideOthers" },
            { type: "separator" },
            { role: "quit" },
          ],
        }] satisfies MenuItemConstructorOptions[])
      : []),
    {
      label: "File",
      submenu: [
        {
          label: "Open Folder…",
          accelerator: "CmdOrCtrl+Shift+O",
          click: () => void pickVault(),
        },
        { label: "Open Recent", submenu: recentsSubmenu() },
        { type: "separator" },
        {
          label: isMac ? "Reveal Folder in Finder" : "Show Folder in File Manager",
          enabled: openVault() !== null,
          click: revealVault,
        },
        ...(isMac
          ? []
          : ([
              { type: "separator" },
              { label: "Copy MCP Endpoint URL", enabled: mcpUrl() !== null, click: copyMcpUrl },
              { type: "separator" },
              { label: "Settings…", accelerator: "CmdOrCtrl+,", click: openSettings },
              updateItem(),
              { type: "separator" },
              { role: "quit" },
            ] satisfies MenuItemConstructorOptions[])),
      ],
    },
    { role: "editMenu" },
    {
      label: "View",
      submenu: [
        { role: "reload" },
        { role: "toggleDevTools" },
        { type: "separator" },
        { role: "resetZoom" },
        { role: "zoomIn" },
        { role: "zoomOut" },
        { type: "separator" },
        { role: "togglefullscreen" },
      ],
    },
    { role: "windowMenu" },
    {
      role: "help",
      submenu: [
        {
          label: "Octo Documentation",
          click: () => void shell.openExternal("https://octo.juancavallotti.com"),
        },
        { label: "Show Server Log", click: () => shell.showItemInFolder(logPath()) },
      ],
    },
  ];

  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}
