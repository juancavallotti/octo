"use client";

import { createElement } from "react";
import { X } from "lucide-react";
import type { FlowDoc } from "@/app/model/document";
import { getSourceSpec, resolveIcon } from "@/app/schema";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import SettingsField from "./SettingsField";
import ReferenceField from "./fields/ReferenceField";

/**
 * Settings body for a flow's selected source: a header (icon, label, close), one
 * input per source field from the capability schema, and a remove button. Sources
 * have no slot fields, so every field is editable here. Rendered inside
 * SettingsPanel.
 */
export default function SourceSettings({ flow }: { flow: FlowDoc }) {
  const { dispatch } = useEditorState();
  const source = flow.source;
  const spec =
    source?.connector && source.type
      ? getSourceSpec(source.connector, source.type)
      : undefined;

  if (!source || !spec) {
    return (
      <div className="flex flex-1 items-center justify-center p-6 text-center text-sm text-zinc-400 dark:text-zinc-500">
        Unknown source type.
      </div>
    );
  }

  return (
    <>
      <header className="flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
        {createElement(resolveIcon(spec.icon ?? ""), {
          size: 18,
          className: "text-zinc-500 shrink-0",
        })}
        <span className="font-semibold tracking-tight truncate">
          {spec.label}
        </span>
        <button
          type="button"
          aria-label="Close settings"
          onClick={() =>
            dispatch({
              type: EditorActionType.SELECT_SOURCE,
              data: { flowId: null },
            })
          }
          className="ml-auto rounded-full p-1 text-zinc-400 transition-colors hover:text-zinc-700 dark:hover:text-zinc-200"
        >
          <X size={16} />
        </button>
      </header>

      <div className="flex flex-col gap-4 overflow-y-auto p-4">
        {source.connector && (
          <div className="flex flex-col gap-1">
            <label className="text-xs font-medium text-zinc-600 dark:text-zinc-300">
              Connector
            </label>
            <ReferenceField
              spec={{ kind: "connector", connectorType: source.connector }}
              value={source.connectorRef}
              required={false}
              onChange={(value) =>
                dispatch({
                  type: EditorActionType.UPDATE_SOURCE_CONNECTOR,
                  data: { flowId: flow.id, connector: value as string | undefined },
                })
              }
            />
            <p className="text-xs text-zinc-400 dark:text-zinc-500">
              Which {source.connector} connection drives this flow.
            </p>
          </div>
        )}

        {spec.fields.map((field) => (
          <SettingsField
            key={`${flow.id}:${field.name}`}
            field={field}
            value={source.settings[field.name]}
            onChange={(value) =>
              dispatch({
                type: EditorActionType.UPDATE_SOURCE_SETTING,
                data: { flowId: flow.id, field: field.name, value },
              })
            }
          />
        ))}
      </div>
    </>
  );
}
