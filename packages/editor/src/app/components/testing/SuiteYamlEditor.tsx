"use client";

import Editor from "react-simple-code-editor";
import { highlight } from "../resources/highlight";

/**
 * The raw YAML of one suite, editable.
 *
 * The string is the source of truth here — what is typed is what is stored, byte for
 * byte. That matters because these files are committed and heavily commented, and the
 * model cannot round-trip a comment: anything that re-serialized on the way through
 * would quietly reformat a file the user wrote by hand. The form view (F2) is the one
 * that re-emits, and it warns before it does.
 *
 * Highlighting reuses the Resources tab's Prism setup, addressed by a `.yaml` name so it
 * picks the YAML grammar.
 */
export default function SuiteYamlEditor({
  value,
  onChange,
  readOnly,
}: {
  value: string;
  onChange: (next: string) => void;
  readOnly?: boolean;
}) {
  return (
    <div className="min-h-0 flex-1 overflow-auto">
      <Editor
        value={value}
        highlight={(code) => highlight(code, "suite.yaml")}
        onValueChange={readOnly ? () => {} : onChange}
        readOnly={readOnly}
        padding={12}
        textareaClassName="octo-code-textarea"
        preClassName="octo-code"
        className="octo-code min-h-full"
        textareaId="suite-yaml"
        // Set the mono font via style, not a class: react-simple-code-editor copies
        // these onto both the textarea and the highlight <pre> so their glyph metrics
        // line up exactly. An "inherit" here would fall back to the app's sans font.
        style={{
          fontFamily:
            "ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace",
          fontSize: 12,
          lineHeight: 1.6,
          minHeight: "100%",
        }}
      />
    </div>
  );
}
