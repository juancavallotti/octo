/**
 * Kind inference and starter content for newly created resources. Mirrors the
 * platform ResourcesSection's guessKind so a hand-created file lands in the right
 * runtime kind, and gives each file type a minimal scaffold so a new file opens
 * with something sensible rather than blank.
 */

/** The runtime kind for a name: env-convention files are env, everything else a template. */
export function guessKind(name: string): "env" | "template" {
  const base = (name.toLowerCase().split("/").pop() ?? "").trim();
  return base === ".env" || base.startsWith(".env.") || base.endsWith(".env")
    ? "env"
    : "template";
}

const HTML_SKELETON = `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <title></title>
  </head>
  <body>
  </body>
</html>
`;

/** Starter content for a new file, chosen by extension. */
export function scaffoldFor(name: string): string {
  const lower = name.toLowerCase();
  if (lower.endsWith(".json")) return "{}\n";
  if (lower.endsWith(".html") || lower.endsWith(".htm")) return HTML_SKELETON;
  if (lower.endsWith(".xml") || lower.endsWith(".svg")) {
    return '<?xml version="1.0" encoding="UTF-8"?>\n';
  }
  if (lower.endsWith(".yaml") || lower.endsWith(".yml")) return "# \n";
  if (guessKind(name) === "env") return "# KEY=value\n";
  return "";
}
