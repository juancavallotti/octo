import Prism from "prismjs";

/**
 * Prism syntax highlighting for CEL expressions, mirroring the approach in
 * `components/resources/highlight.ts`: we reuse the Prism instance the rest of the
 * editor ships and define a small hand-rolled grammar instead of importing a full
 * language. Token names are Prism-standard so they reuse the shared `.octo-code` /
 * `.octo-cel` token colors in editor.css (with a few CEL additions: keyword,
 * function, operator, variable).
 *
 * CEL grammar reference: https://celbyexample.com/
 */

/**
 * The CEL grammar. Order matters — Prism tries patterns top-to-bottom, so strings
 * and numbers are consumed before operators, and a `name(` function call is matched
 * before the same name could be read as a bare identifier/variable.
 */
export const celGrammar: Prism.Grammar = {
  // CEL has no line comments in message expressions, but tolerate `//` defensively.
  comment: { pattern: /\/\/.*/, greedy: true },
  string: {
    // Single- or double-quoted, with escapes; raw (r"...") prefix tolerated.
    pattern: /r?(["'])(?:\\.|(?!\1)[^\\\r\n])*\1/,
    greedy: true,
  },
  number:
    /\b0x[\da-fA-F]+\b|(?:\b\d+(?:\.\d*)?|\B\.\d+)(?:[eE][+-]?\d+)?[uU]?\b/,
  boolean: /\b(?:true|false|null)\b/,
  // A call: identifier immediately followed by `(` (macros and functions alike).
  function: /\b[A-Za-z_]\w*(?=\s*\()/,
  keyword: /\b(?:in)\b/,
  // The in-scope message variables get their own color; other identifiers stay plain.
  variable: /\b(?:body|vars|env|eventID|correlationID|now)\b/,
  operator: /\|\||&&|[<>]=?|[!=]=|!|\?|:|[-+*/%]|\./,
  punctuation: /[()[\]{},]/,
};

/** Highlight a CEL expression to HTML using {@link celGrammar}. */
export function highlightCel(code: string): string {
  return Prism.highlight(code, celGrammar, "cel");
}
