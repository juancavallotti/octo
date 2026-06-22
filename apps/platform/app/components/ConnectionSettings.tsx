"use client";

import { createElement } from "react";
import { X } from "lucide-react";
import type { ConnectorInstance } from "@/app/model/document";
import { duplicateNames, slugify } from "@/app/model/identity";
import { getConnectorSpec, resolveIcon } from "@/app/schema";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import SettingsField from "./SettingsField";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * Settings body for a selected connection (connector instance): a header, an
 * editable slug-style Name (the reference other nodes resolve against), and one
 * input per connector setting from the capability schema. The name auto-slugifies
 * as you type and warns when it collides with another connection. Rendered inside
 * SettingsPanel, mirroring SourceSettings.
 */
export default function ConnectionSettings({
  connection,
}: {
  connection: ConnectorInstance;
}) {
  const { state, dispatch } = useEditorState();
  const spec = getConnectorSpec(connection.type);

  const dupes = duplicateNames(state.document.connectors.map((c) => c.name));
  const duplicate = dupes.has(connection.name);

  return (
    <>
      <header className="flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
        {createElement(resolveIcon(spec?.icon ?? ""), {
          size: 18,
          className: "text-zinc-500 shrink-0",
        })}
        <span className="font-semibold tracking-tight truncate">
          {spec?.label ?? connection.type}
        </span>
        <button
          type="button"
          aria-label="Close settings"
          onClick={() =>
            dispatch({
              type: EditorActionType.SELECT_CONNECTION,
              data: { id: null },
            })
          }
          className="ml-auto rounded-full p-1 text-zinc-400 transition-colors hover:text-zinc-700 dark:hover:text-zinc-200"
        >
          <X size={16} />
        </button>
      </header>

      <div className="flex flex-col gap-4 overflow-y-auto p-4">
        <div className="flex flex-col gap-1">
          <label
            htmlFor="connection-name"
            className="text-xs font-medium text-zinc-600 dark:text-zinc-300"
          >
            Name
          </label>
          <input
            id="connection-name"
            type="text"
            value={connection.name}
            onChange={(e) =>
              dispatch({
                type: EditorActionType.RENAME_CONNECTION,
                data: { id: connection.id, name: slugify(e.target.value) },
              })
            }
            className={INPUT}
          />
          {duplicate ? (
            <p className="text-xs text-red-500">
              Another connection already uses this name.
            </p>
          ) : (
            <p className="text-xs text-zinc-400 dark:text-zinc-500">
              Referenced by name from sources and blocks.
            </p>
          )}
        </div>

        {spec?.settings.map((field) => (
          <SettingsField
            key={`${connection.id}:${field.name}`}
            field={field}
            value={connection.settings[field.name]}
            onChange={(value) =>
              dispatch({
                type: EditorActionType.UPDATE_CONNECTION_SETTING,
                data: { id: connection.id, field: field.name, value },
              })
            }
          />
        ))}
      </div>
    </>
  );
}
