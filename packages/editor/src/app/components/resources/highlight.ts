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

/** Matches `{{ ... }}` template expressions (non-greedy, spanning newlines). */
const HANDLEBARS_RE = /\{\{[\s\S]*?\}\}/g;

/** Highlight a segment with the base grammar, or escape it for plain text. */
function highlightBase(
  text: string,
  lang: { grammar: Prism.Grammar; language: string } | null,
): string {
  if (text === "") return "";
  return lang
    ? Prism.highlight(text, lang.grammar, lang.language)
    : escapeHtml(text);
}

/**
 * Highlight `content` for the resource `name`, returning HTML. `{{ ... }}`
 * template expressions are marked in *every* language — including plain text and
 * inside strings — by splitting them out and highlighting the surrounding
 * segments with the base grammar. (Merging a handlebars token into the grammar
 * wouldn't work: a language's own string/attribute token swallows the braces
 * first.) Non-token text is escaped, so plain files render verbatim.
 */
export function highlight(content: string, name: string): string {
  const lang = languageFor(name);
  let out = "";
  let last = 0;
  HANDLEBARS_RE.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = HANDLEBARS_RE.exec(content)) !== null) {
    out += highlightBase(content.slice(last, match.index), lang);
    out += `<span class="token handlebars">${escapeHtml(match[0])}</span>`;
    last = match.index + match[0].length;
  }
  out += highlightBase(content.slice(last), lang);
  return out;
}

/** A short human label for the file's language (for a status hint). */
export function languageLabel(name: string): string {
  const lang = languageFor(name);
  if (!lang) return "plain text";
  return { env: "env", json: "JSON", markup: "markup", yaml: "YAML" }[
    lang.language
  ] ?? lang.language;
}
