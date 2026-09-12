#!/usr/bin/env node
// Fails CI when the editor's CEL scope model drifts from the runtime registry.
//
// Source of truth: `bin/octo schema`. The editor infers what a block puts in scope
// from its settings (packages/editor/src/app/scope/contributions.ts), in two tiers: a
// naming convention read straight from the schema (`resultVar`, `statusVar`, `as`,
// `name`, …), and a small hand-written table for blocks whose effect on the message
// the schema cannot express.
//
// The convention tier needs no maintenance. The table does, and nothing about a new
// block announces that it belongs there — the schema says what a block's settings
// are, never what it does to the message. What this check can do is notice a block
// arriving with a variable-naming setting the convention has not seen before, which
// is the moment to decide whether the convention reads it correctly.
//
// Deliberate decisions go in scripts/scope-contributions-allowlist.json:
//   { "conventional": ["type-a"] }  — the convention reads this block correctly
//
// Usage: node scripts/check-scope-contributions.mjs  (requires bin/octo; build with
// `task runtime:build`)

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const octoBin = join(repo, 'bin', 'octo');
const contributions = join(repo, 'packages/editor/src/app/scope/contributions.ts');
const allowlistPath = join(repo, 'scripts', 'scope-contributions-allowlist.json');

if (!existsSync(octoBin)) {
  console.error(`missing ${octoBin} — run \`task runtime:build\` first`);
  process.exit(2);
}

const schema = JSON.parse(
  execFileSync(octoBin, ['schema'], { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 }),
);
const blocks = schema.blocks ?? [];
if (blocks.length === 0) {
  console.error('octo schema returned no blocks — refusing to pass vacuously');
  process.exit(2);
}

// The same pattern contributions.ts uses. Kept in step by this check failing loudly
// if it is changed there and not here.
const NAMING = /(^as$)|(^name$)|(Var$)/;

// The hand-written table's keys, read out of the source rather than imported: this is
// a plain Node script and that file is TypeScript inside a workspace package.
const source = readFileSync(contributions, 'utf8');
// Anchored to the SPECIAL table rather than scanned over the whole file. A loose scan
// picks up the keys of any other object-of-functions in there, and those keys can
// equal real block types — which would mark a block as accounted for that nobody ever
// looked at. A drift check that fails open is worse than no check.
// The declaration carries a function type, so its `=>` defeats a naive [^=]*.
const specialBlock = /const SPECIAL\b[^\n]*=\s*\{\r?\n([\s\S]*?)\r?\n\};/.exec(source);
if (!specialBlock) {
  console.error(`could not find the SPECIAL table in ${contributions} — refusing to guess`);
  process.exit(2);
}
const special = new Set(
  [...specialBlock[1].matchAll(/^\s{2}"?([a-z0-9-]+)"?:\s*\(/gm)].map((m) => m[1]),
);

const allowlist = existsSync(allowlistPath)
  ? JSON.parse(readFileSync(allowlistPath, 'utf8'))
  : { conventional: [] };
const known = new Set([...special, ...(allowlist.conventional ?? [])]);

const unaccounted = blocks
  .filter((b) => !known.has(b.type))
  .filter((b) => (b.fields ?? []).some((f) => f.type === 'string' && NAMING.test(f.name)))
  .map((b) => ({
    type: b.type,
    fields: b.fields.filter((f) => f.type === 'string' && NAMING.test(f.name)).map((f) => f.name),
  }));

if (unaccounted.length > 0) {
  console.error('Blocks declare variables the editor\'s scope model has not been told about:\n');
  for (const b of unaccounted) console.error(`  ${b.type}  (${b.fields.join(', ')})`);
  console.error(`
Each one sets a variable the CEL completion will now offer. Decide which is true:

  - The convention reads it correctly (the setting's value IS the variable name, and
    its shape is dyn or one of the KNOWN_SHAPES). Add the type to
    scripts/scope-contributions-allowlist.json under "conventional".
  - It needs more than that — it replaces the body, scopes the variable to a slot,
    or sets something the convention cannot see. Add it to SPECIAL in
    packages/editor/src/app/scope/contributions.ts.
`);
  process.exit(1);
}

console.log(`scope contributions: ${blocks.length} blocks, ${known.size} accounted for explicitly`);
