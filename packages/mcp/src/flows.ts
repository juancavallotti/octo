/**
 * Reading and rewriting ONE flow inside an integration's definition, leaving every other
 * byte of the file exactly as it was.
 *
 * An integration is one YAML file holding many flows, and the only way to change one used
 * to be `update_integration`, which overwrites the whole thing. That makes an agent
 * editing a single flow reproduce every other flow, every connector, and every comment
 * from memory — and quietly lose whatever it got wrong. These functions exist so it
 * doesn't have to.
 *
 * ## Why this is a textual splice, and not an AST rewrite
 *
 * The obvious implementation — parse to a `Document`, swap one item of the `flows`
 * sequence, stringify — **does not work**, and it fails in a way that is easy to miss
 * because it only shows up on real files.
 *
 * Stringifying a document re-serializes *every* node from the AST, not just the one that
 * changed. The AST does not remember how a scalar was written, so the printer re-decides:
 * long single-quoted CEL expressions get folded at the line width, and folded block
 * scalars (`prompt: >`) get flattened onto one line. Those rewrites land on flows the
 * caller never touched. A tool whose entire purpose is "stop clobbering the parts you
 * weren't asked to touch" cannot be built on a printer that clobbers them — the damage
 * would surface as a noisy diff in the user's repo, attributed to an edit they never made.
 *
 * So the document is parsed only to *locate* the flow. Each node carries a `range` into
 * the source text, and the edit is then done on the string: take the bytes before the
 * flow, the bytes after it, and put the new flow between them. Everything outside that
 * range is not re-printed — it is the same bytes it always was. Comments above a flow,
 * the blank lines between flows, and the formatting of every other flow are preserved not
 * by effort but by construction.
 *
 * The flow being written is likewise inserted verbatim, so a block scalar or a comment the
 * caller wrote in it survives into the file too.
 */

import { isMap, isSeq, parseDocument, type Document, type YAMLMap, type YAMLSeq } from "yaml";

/** A node's span in the source text: `[start, valueEnd, nodeEnd]`. */
type Range = [number, number, number];

/** A parsed definition, kept alongside the source the ranges point into. */
interface Parsed {
  src: string;
  doc: Document;
  seq: YAMLSeq | null;
}

/**
 * Parse the definition and locate its `flows` sequence. `seq` is null when the definition
 * declares no flows at all, or declares an empty list — both are legitimate starting
 * points for `add_flow`, and neither has any text to splice into.
 */
function parse(definition: string): Parsed {
  const doc = parseDocument(definition);
  if (doc.errors.length > 0) {
    throw new Error(`the definition is not valid YAML: ${doc.errors[0].message}`);
  }
  if (!isMap(doc.contents)) {
    throw new Error("the definition is not a YAML mapping");
  }

  const flows = doc.get("flows");
  if (flows === undefined || flows === null) return { src: definition, doc, seq: null };
  if (!isSeq(flows)) {
    throw new Error("the definition's `flows` is not a list");
  }
  return { src: definition, doc, seq: flows.items.length > 0 ? flows : null };
}

/** The `name` of a flow node, or null when it has none (or isn't a mapping). */
function nameOf(node: unknown): string | null {
  if (!isMap(node)) return null;
  const name = (node as YAMLMap).get("name");
  return typeof name === "string" ? name : null;
}

/** The flow names in a parsed definition, in file order. */
function namesOf(seq: YAMLSeq | null): string[] {
  if (!seq) return [];
  return seq.items.map(nameOf).filter((n): n is string => n !== null);
}

/** The index of the flow named `name`, or -1. */
function indexOf(seq: YAMLSeq | null, name: string): number {
  if (!seq) return -1;
  return seq.items.findIndex((item) => nameOf(item) === name);
}

/**
 * Where one flow lives in the source text.
 *
 * `start` is the beginning of the line the item opens on — so the region includes the
 * `  - ` bullet, which the node's own range starts *after*. `bullet` is that prefix, and
 * its width is the continuation indent every later line of the flow carries.
 */
interface Span {
  /** Offset of the first character of the item's opening line. */
  start: number;
  /** Offset just past the item's final newline. */
  end: number;
  /** The `  - ` prefix opening the item. */
  bullet: string;
}

function spanOf(src: string, item: unknown): Span {
  const [contentStart, valueEnd] = (item as { range: Range }).range;
  const start = src.lastIndexOf("\n", contentStart - 1) + 1;
  return { start, end: valueEnd, bullet: src.slice(start, contentStart) };
}

/**
 * Render a flow's YAML as a sequence item under `bullet`: the first line after the bullet
 * itself, every later line indented to match. Blank lines are left blank rather than
 * padded with trailing spaces.
 */
function asItem(flowYaml: string, bullet: string): string {
  const indent = " ".repeat(bullet.length);
  const lines = flowYaml.replace(/\s+$/, "").split("\n");
  return (
    lines
      .map((line, i) => (i === 0 ? bullet + line : line === "" ? "" : indent + line))
      .join("\n") + "\n"
  );
}

/**
 * Check one flow's YAML and return it verbatim. It must be a mapping with a `name`,
 * because `name` is the only handle anything has on a flow: it is how a `flow-ref`
 * addresses one, how `invoke_flow` calls one, and how these tools find one again.
 *
 * The text is returned unparsed — what goes into the file is what the caller wrote, so
 * their block scalars and comments survive exactly as they typed them.
 */
function checkFlow(flowYaml: string): { text: string; name: string } {
  const sub = parseDocument(flowYaml);
  if (sub.errors.length > 0) {
    throw new Error(`the flow is not valid YAML: ${sub.errors[0].message}`);
  }
  if (!isMap(sub.contents)) {
    throw new Error(
      "a flow must be a YAML mapping (`name:` and `process:`) — pass the flow itself, not the whole definition and not a `flows:` list",
    );
  }
  const name = nameOf(sub.contents);
  if (!name) throw new Error("the flow needs a `name`");
  return { text: flowYaml, name };
}

/** The names of every top-level flow in the definition, in file order. */
export function flowNames(definition: string): string[] {
  return namesOf(parse(definition).seq);
}

/**
 * The YAML of one named flow, exactly as it appears in the file — its comments, its block
 * scalars, its formatting. Dedented out of the `flows` list so it can be handed straight
 * back to {@link updateFlow}.
 */
export function getFlow(definition: string, name: string): string {
  const { src, seq } = parse(definition);
  const at = indexOf(seq, name);
  if (at < 0) throw new Error(missing(name, seq));

  // Every line of the item carries the bullet's width — the opening line as `  - `, the
  // rest as plain indent — so dedenting by it yields the flow on its own.
  const span = spanOf(src, seq!.items[at]);
  return src
    .slice(span.start, span.end)
    .split("\n")
    .map((line) => line.slice(span.bullet.length))
    .join("\n");
}

/**
 * Append a flow to the definition. Refuses a name that is already taken: a flow the caller
 * believed was new but which already exists is a mistake worth hearing about, and two
 * flows of one name is a config the runtime cannot address unambiguously.
 */
export function addFlow(definition: string, flowYaml: string): string {
  const { src, seq } = parse(definition);
  const { text, name } = checkFlow(flowYaml);
  if (indexOf(seq, name) >= 0) {
    throw new Error(
      `a flow named "${name}" already exists — use update_flow to change it, or rename this one`,
    );
  }

  // With flows already there, put it after the last one, a blank line apart — butted
  // straight up against the flow above it, a new flow reads as part of it.
  if (seq) {
    const last = spanOf(src, seq.items[seq.items.length - 1]);
    const item = asItem(text, last.bullet);
    return src.slice(0, last.end) + "\n" + item + src.slice(last.end);
  }

  // No flows yet: either the key is absent, or it is there but empty. A config declaring
  // only connectors is a legitimate thing to add the first flow to.
  const item = asItem(text, "  - ");
  const empty = /^flows:[ \t]*(\[[ \t]*\])?[ \t]*$/m.exec(src);
  if (empty) {
    const after = src.slice(empty.index + empty[0].length).replace(/^\n/, "");
    return src.slice(0, empty.index) + "flows:\n" + item + after;
  }
  return src.replace(/\n*$/, "\n") + "\nflows:\n" + item;
}

/**
 * Replace the flow named `name`, in place. Everything else in the file — the other flows,
 * the connectors, the comments, the blank lines — is left byte for byte as it was.
 *
 * The replacement may carry a different `name`, which renames the flow where it sits. That
 * is deliberate: a rename is a normal edit, and forcing it through delete+add would move
 * the flow to the end of the file for no reason. A rename onto a name another flow already
 * holds is still refused.
 */
export function updateFlow(definition: string, name: string, flowYaml: string): string {
  const { src, seq } = parse(definition);
  const at = indexOf(seq, name);
  if (at < 0) throw new Error(missing(name, seq));

  const { text, name: newName } = checkFlow(flowYaml);
  if (newName !== name && indexOf(seq, newName) >= 0) {
    throw new Error(`cannot rename "${name}" to "${newName}": another flow already has that name`);
  }

  const span = spanOf(src, seq!.items[at]);
  return src.slice(0, span.start) + asItem(text, span.bullet) + src.slice(span.end);
}

/** Remove the flow named `name`. Throws when there is none. */
export function deleteFlow(definition: string, name: string): string {
  const { src, seq } = parse(definition);
  const at = indexOf(seq, name);
  if (at < 0) throw new Error(missing(name, seq));

  const span = spanOf(src, seq!.items[at]);
  let { start, end } = span;

  // Flows sit a blank line apart, and removing one has to take a separator with it or the
  // gap it leaves behind doubles. Which one depends on where it sat: a flow with another
  // below it gives up the blank line under it; the last flow gives up the one above it,
  // so the file does not end on a run of blank lines.
  const last = at === seq!.items.length - 1;
  if (last) {
    while (start > 0 && /^[ \t]*\n$/.test(src.slice(src.lastIndexOf("\n", start - 2) + 1, start))) {
      start = src.lastIndexOf("\n", start - 2) + 1;
    }
  } else {
    while (end < src.length && /^[ \t]*\n/.test(src.slice(end))) {
      end = src.indexOf("\n", end) + 1;
    }
  }
  return src.slice(0, start) + src.slice(end);
}

/** "No such flow", naming the ones that do exist — the next thing the caller will ask. */
function missing(name: string, seq: YAMLSeq | null): string {
  const names = namesOf(seq);
  const known = names.length > 0 ? names.map((n) => `"${n}"`).join(", ") : "none";
  return `no flow named "${name}" in this integration (it has: ${known})`;
}
