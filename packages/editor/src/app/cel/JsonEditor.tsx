"use client";

import { useEffect, useRef } from "react";
import Editor from "react-simple-code-editor";
import Prism from "prismjs";
import "prismjs/components/prism-json";
import { autoPair } from "./autoPair";

/**
 * A small syntax-highlighted JSON input (react-simple-code-editor + Prism), used for
 * the CEL tester's body/vars/env sample fields. It reuses the shared `.octo-code`
 * token theming and the mono-font setup from the resource/CEL editors, and shares the
 * bracket/quote auto-pairing used by the CEL editor. No completion.
 */

const MONO_FONT =
  "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace";

const EDITOR_STYLE: React.CSSProperties = {
  fontFamily: MONO_FONT,
  fontSize: 12,
  lineHeight: 1.6,
};

function highlightJson(code: string): string {
  return Prism.highlight(code, Prism.languages.json, "json");
}

export interface JsonEditorProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  minHeight?: number;
}

export default function JsonEditor({
  value,
  onChange,
  placeholder,
  minHeight = 72,
}: JsonEditorProps) {
  const taRef = useRef<HTMLTextAreaElement | null>(null);
  const pendingCaret = useRef<number | null>(null);

  // Restore the caret after an auto-pair edit re-renders with the new value.
  useEffect(() => {
    if (pendingCaret.current != null && taRef.current) {
      const pos = pendingCaret.current;
      pendingCaret.current = null;
      taRef.current.setSelectionRange(pos, pos);
    }
  });

  function handleKeyDown(
    e: React.KeyboardEvent<HTMLTextAreaElement | HTMLDivElement>,
  ) {
    const ta = e.currentTarget as HTMLTextAreaElement;
    taRef.current = ta;
    const paired = autoPair(value, ta.selectionStart, ta.selectionEnd, e.key);
    if (paired) {
      e.preventDefault();
      pendingCaret.current = paired.caret;
      onChange(paired.value);
    }
  }

  return (
    <div className="rounded-md border border-black/10 bg-transparent focus-within:border-black/30 dark:border-white/15 dark:focus-within:border-white/30">
      <Editor
        value={value}
        onValueChange={onChange}
        highlight={highlightJson}
        onKeyDown={handleKeyDown}
        onFocus={(e) => {
          taRef.current = e.currentTarget as HTMLTextAreaElement;
        }}
        placeholder={placeholder}
        padding={8}
        preClassName="octo-code"
        textareaClassName="octo-code-textarea"
        style={{ ...EDITOR_STYLE, minHeight }}
      />
    </div>
  );
}
