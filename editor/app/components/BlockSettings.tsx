"use client";

import { createElement } from "react";
import { X } from "lucide-react";
import type { BlockNode } from "@/app/model/document";
import { isSlotField, slotFields } from "@/app/model/document";
import { getBlockSpec, resolveIcon } from "@/app/schema";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import SettingsField from "./SettingsField";
import SlotListEditor from "./SlotListEditor";

/** Slot fields holding a list of paths the panel can grow/shrink (cases/branches). */
const LIST_SLOT_TYPES = new Set(["case-list", "flow-list"]);

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * Settings body for a selected canvas block: a header (icon, label, close), the
 * step name, and one input per editable (non-slot) field from the capability
 * schema. Rendered inside SettingsPanel.
 */
export default function BlockSettings({ block }: { block: BlockNode }) {
  const { dispatch } = useEditorState();
  const spec = getBlockSpec(block.type);

  if (!spec) {
    return (
      <div className="flex flex-1 items-center justify-center p-6 text-center text-sm text-zinc-400 dark:text-zinc-500">
        Unknown component type “{block.type}”.
      </div>
    );
  }

  const fields = spec.fields.filter((f) => !isSlotField(f));
  const listSlots = slotFields(block.type).filter((f) =>
    LIST_SLOT_TYPES.has(f.type),
  );

  return (
    <>
      <header className="flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
        {createElement(resolveIcon(spec.icon), {
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
              type: EditorActionType.SELECT_BLOCK,
              data: { blockId: null },
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
            htmlFor="block-name"
            className="text-xs font-medium text-zinc-600 dark:text-zinc-300"
          >
            Name
          </label>
          <input
            id="block-name"
            type="text"
            value={block.name ?? ""}
            placeholder={spec.label}
            onChange={(e) =>
              dispatch({
                type: EditorActionType.RENAME_BLOCK,
                data: { blockId: block.id, name: e.target.value },
              })
            }
            className={INPUT}
          />
        </div>

        {fields.length === 0 && listSlots.length === 0 ? (
          <p className="text-xs text-zinc-400 dark:text-zinc-500">
            This component has no settings.
          </p>
        ) : (
          fields.map((field) => (
            <SettingsField
              key={`${block.id}:${field.name}`}
              field={field}
              value={block.settings[field.name]}
              onChange={(value) =>
                dispatch({
                  type: EditorActionType.UPDATE_BLOCK_SETTING,
                  data: { blockId: block.id, field: field.name, value },
                })
              }
            />
          ))
        )}

        {listSlots.map((field) => (
          <SlotListEditor
            key={`${block.id}:${field.name}`}
            block={block}
            field={field}
          />
        ))}
      </div>
    </>
  );
}
