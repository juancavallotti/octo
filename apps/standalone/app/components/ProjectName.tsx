"use client";

import { FolderOpen } from "lucide-react";
import { BAR_BUTTON, useEditorState } from "@octo/editor";

/**
 * The open project's name in the document bar: the folder this app serves, named by
 * the `service.name` its files declare. Switching projects is switching folders, which
 * the desktop app does and a plain server does not.
 */
export default function ProjectName() {
  const { state } = useEditorState();
  return (
    <span className={`${BAR_BUTTON} min-w-0`}>
      <FolderOpen size={15} className="shrink-0" />
      <span className="truncate">{state.project.name || "untitled-project"}</span>
    </span>
  );
}
