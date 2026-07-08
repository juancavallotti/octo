"use client";

import Editor from "react-simple-code-editor";
import { highlightCel } from "./highlight";
import CompletionMenu from "./CompletionMenu";
import { useCelCompletion, WHOLE_TEXT_SCOPE } from "./useCelCompletion";

/**
 * A single-field CEL editor: a syntax-highlighted text input (react-simple-code-editor
 * + Prism, mirroring the Resources editor) with a lightweight completion menu and
 * IDE-style hover docs driven by the CEL catalogue. It replaces the plain `<textarea>`
 * used for `cel` fields and `expr` inputs.
 *
 * Completion is deliberately basic (issue #125): prefix-match on the in-scope
 * variables and catalogued functions, triggered as you type (or on Ctrl/Cmd+Space).
 * The whole field is CEL — see the template `{{ }}` editor for scoped completion.
 * The menu anchors below the field — caret-accurate placement is a possible follow-up.
 */

const MONO_FONT =
  "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace";

/** react-simple-code-editor copies these onto both the textarea and the highlight
 * <pre> so their glyph metrics line up; the mono font must be set here, not via a
 * class, or the <pre> would fall back to the app's sans font. */
const EDITOR_STYLE: React.CSSProperties = {
  fontFamily: MONO_FONT,
  fontSize: 12,
  lineHeight: 1.6,
};

export interface CelEditorProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  /** Minimum height in px (defaults to ~2 lines). */
  minHeight?: number;
  /** Forwarded to the textarea for label association. */
  id?: string;
  disabled?: boolean;
}

export default function CelEditor({
  value,
  onChange,
  placeholder = "CEL expression",
  minHeight = 40,
  id,
  disabled,
}: CelEditorProps) {
  const { menu, selected, setSelected, accept, handlers } = useCelCompletion({
    value,
    onChange,
    scope: WHOLE_TEXT_SCOPE,
  });

  return (
    <div className="relative">
      <div className="rounded-md border border-black/10 bg-transparent focus-within:border-black/30 dark:border-white/15 dark:focus-within:border-white/30">
        <Editor
          value={value}
          highlight={highlightCel}
          onValueChange={handlers.onValueChange}
          onKeyDown={handlers.onKeyDown}
          onKeyUp={handlers.onKeyUp}
          onClick={handlers.onClick}
          onFocus={handlers.onFocus}
          onBlur={handlers.onBlur}
          placeholder={placeholder}
          textareaId={id}
          disabled={disabled}
          padding={8}
          preClassName="octo-cel"
          textareaClassName="octo-cel-textarea"
          style={{ ...EDITOR_STYLE, minHeight }}
        />
      </div>

      {menu && (
        <CompletionMenu
          items={menu.items}
          selected={selected}
          onHover={setSelected}
          onAccept={accept}
          position={menu.pos}
        />
      )}
    </div>
  );
}
