#!/usr/bin/env node
// Fails CI when the editor's list of side-effect-free blocks drifts from the runtime.
//
// packages/editor/src/app/run/pure.ts names the block types the editor may run on its
// own, in the background, to learn what a flow's messages look like. Being wrong about
// one is not a completion bug: it means the editor called somebody's API, or sent a
// message, because a field was edited.
//
// The list fails closed — an unknown type is never eligible — so a block *added* to the
// runtime needs nothing here. What this checks is the other direction, where failing
// closed does not help:
//
//   1. A listed type no longer exists. The list is then a lie about a block that was
//      renamed, and the rename probably needs following.
//   2. A listed type has grown a connector reference. That is the schema saying the
//      block can now reach something outside the process, and it must leave the list.
//
// Usage: node scripts/check-pure-blocks.mjs  (requires bin/octo; build with
// `task runtime:build`)

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const octoBin = join(repo, 'bin', 'octo');
const pureFile = join(repo, 'packages/editor/src/app/run/pure.ts');

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

// Read out of the source rather than imported: this is a plain Node script and that
// file is TypeScript inside a workspace package.
const source = readFileSync(pureFile, 'utf8');
const listed = new Set(
  [...source.matchAll(/^\s*"([a-z0-9-]+)",$/gm)].map((m) => m[1]),
);
if (listed.size === 0) {
  console.error(`parsed no block types out of ${pureFile} — refusing to pass vacuously`);
  process.exit(2);
}

/**
 * Blocks allowed to stay on the list despite naming a connector, with the reason. A
 * logger is a sink for the run's own diagnostics; the worst a background run does
 * through one is write lines nobody asked for.
 */
const CONNECTOR_OK = new Map([['log', 'a logger is a sink for diagnostics, not a call out']]);

const byType = new Map(blocks.map((b) => [b.type, b]));

function connectorRefs(fields = []) {
  const found = [];
  for (const f of fields) {
    if (f.ref?.kind === 'connector') found.push(f.name);
    if (f.fields) found.push(...connectorRefs(f.fields));
  }
  return found;
}

const missing = [...listed].filter((type) => !byType.has(type));
const reaching = [...listed]
  .filter((type) => byType.has(type) && !CONNECTOR_OK.has(type))
  .map((type) => ({ type, refs: connectorRefs(byType.get(type).fields) }))
  .filter((b) => b.refs.length > 0);

let failed = false;

if (missing.length > 0) {
  failed = true;
  console.error("The editor treats these as side-effect free, but the runtime has no such block:\n");
  for (const type of missing) console.error(`  ${type}`);
  console.error(`
Probably a rename. Follow it in packages/editor/src/app/run/pure.ts, or drop the entry
if the block is gone — a stale name silently costs the flows that use its replacement.
`);
}

if (reaching.length > 0) {
  failed = true;
  console.error('These are on the editor\'s side-effect-free list and now name a connector:\n');
  for (const b of reaching) console.error(`  ${b.type}  (${b.refs.join(', ')})`);
  console.error(`
A connector is something outside this process. Remove the type from PURE_BLOCKS in
packages/editor/src/app/run/pure.ts — the editor runs these flows unasked, and the cost
of being wrong is a real call the user did not make. If the connector genuinely cannot
be observed from outside (as a logger cannot), add it to CONNECTOR_OK in this script
with the reason.
`);
}

if (failed) process.exit(1);

console.log(`pure blocks: ${listed.size} listed, all present and none reaching out`);
