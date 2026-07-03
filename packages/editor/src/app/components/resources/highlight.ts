import Prism from "prismjs";
import "prismjs/components/prism-yaml";
import "prismjs/components/prism-json";
import "prismjs/components/prism-markup"; // covers html + xml

/**
 * Language support for the Resources code editor. We reuse the Prism instance the
 * YAML preview already ships and highlight the file types the tab supports —
 * env, json, html, xml, yaml — falling back to escaped plain text for everything
 * else. Token colors are themed via the shared `.octo-code` classes in editor.css.
 */

/**
 * A minimal grammar for `.env`-style files: `#` comments and the KEY of each
 * `KEY=value` line (Prism ships no dotenv grammar). Registered once; the value is
 * left unstyled so secrets don't get visually chopped up.
 */
const envGrammar: Prism.Grammar = {
  comment: /#.*/,
  key: {
    pattern: /^\s*(?:export\s+)?[\w.-]+(?=\s*=)/m,
    alias: "atrule",
  },
  punctuation: /=/,
};

/** Resolve a resource name to a Prism grammar + language id, or null for plain. */
export function languageFor(
  name: string,
): { grammar: Prism.Grammar; language: string } | null {
  const lower = name.toLowerCase();
  const base = lower.split("/").pop() ?? lower;
  if (base === ".env" || base.startsWith(".env.") || base.endsWith(".env")) {
    return { grammar: envGrammar, language: "env" };
  }
  if (lower.endsWith(".json")) {
    return { grammar: Prism.languages.json, language: "json" };
  }
  if (
    lower.endsWith(".html") ||
    lower.endsWith(".htm") ||
    lower.endsWith(".xml") ||
    lower.endsWith(".svg")
  ) {
    return { grammar: Prism.languages.markup, language: "markup" };
  }
  if (lower.endsWith(".yaml") || lower.endsWith(".yml")) {
    return { grammar: Prism.languages.yaml, language: "yaml" };
  }
  return null;
}

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

/**
 * Highlight `content` for the resource `name`, returning HTML. Plain-text files
 * (and empty content) are HTML-escaped so react-simple-code-editor renders them
 * verbatim without interpreting markup.
 */
export function highlight(content: string, name: string): string {
  const lang = languageFor(name);
  if (!lang) return escapeHtml(content);
  return Prism.highlight(content, lang.grammar, lang.language);
}

/** A short human label for the file's language (for a status hint). */
export function languageLabel(name: string): string {
  const lang = languageFor(name);
  if (!lang) return "plain text";
  return { env: "env", json: "JSON", markup: "markup", yaml: "YAML" }[
    lang.language
  ] ?? lang.language;
}
