#!/usr/bin/env node
// Fails CI when the docs site drifts from the runtime registry.
//
// Source of truth: `bin/octo schema` — the capability catalogue generated from
// the connector/block registries, i.e. exactly what the running binary
// registers. Coverage claim: every registered type must be documented by
// exactly one page under apps/docs/content/docs/ (declared via the page's
// `octo_types` frontmatter list), and every documented type must still be
// registered.
//
// Deliberate exceptions go in scripts/docs-drift-allowlist.json:
//   { "undocumented": ["type-a"], "unregistered": ["type-b"] }
//
// Usage: node scripts/check-docs-drift.mjs  (requires bin/octo; build with
// `task runtime:build`)

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const octoBin = join(repo, 'bin', 'octo');
const docsDir = join(repo, 'apps', 'docs', 'content', 'docs');
const allowlistPath = join(repo, 'scripts', 'docs-drift-allowlist.json');

if (!existsSync(octoBin)) {
  console.error(`missing ${octoBin} — run \`task runtime:build\` first`);
  process.exit(2);
}

// 1. Registered types from the runtime.
const schema = JSON.parse(execFileSync(octoBin, ['schema'], { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 }));
const registered = new Map(); // type -> kind
for (const b of schema.blocks ?? []) registered.set(b.type, 'block');
for (const c of schema.connectors ?? []) registered.set(c.type, 'connector');
if (registered.size === 0) {
  console.error('octo schema returned no blocks/connectors — refusing to pass vacuously');
  process.exit(2);
}

// 2. Documented types from octo_types frontmatter.
function* mdxFiles(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) yield* mdxFiles(full);
    else if (entry.name.endsWith('.mdx')) yield full;
  }
}

// Minimal frontmatter list extraction: octo_types as a YAML block sequence or
// inline JSON-style array. Avoids a YAML dependency.
function extractOctoTypes(text) {
  const fm = text.match(/^---\n([\s\S]*?)\n---/);
  if (!fm) return [];
  const lines = fm[1].split('\n');
  const types = [];
  let inList = false;
  for (const line of lines) {
    const key = line.match(/^octo_types:\s*(.*)$/);
    if (key) {
      const rest = key[1].trim();
      if (rest.startsWith('[')) {
        return rest
          .replace(/^\[|\]$/g, '')
          .split(',')
          .map((s) => s.trim().replace(/^["']|["']$/g, ''))
          .filter(Boolean);
      }
      inList = true;
      continue;
    }
    if (inList) {
      const item = line.match(/^\s+-\s+(.+)$/);
      if (item) types.push(item[1].trim().replace(/^["']|["']$/g, ''));
      else if (line.trim() !== '') inList = false;
    }
  }
  return types;
}

const documented = new Map(); // type -> [files]
for (const file of mdxFiles(docsDir)) {
  for (const t of extractOctoTypes(readFileSync(file, 'utf8'))) {
    const rel = relative(repo, file);
    if (!documented.has(t)) documented.set(t, []);
    documented.get(t).push(rel);
  }
}

// 3. Compare.
const allowlist = existsSync(allowlistPath)
  ? JSON.parse(readFileSync(allowlistPath, 'utf8'))
  : { undocumented: [], unregistered: [] };

const failures = [];
for (const [type, kind] of registered) {
  if (!documented.has(type) && !allowlist.undocumented?.includes(type)) {
    failures.push(`UNDOCUMENTED  ${kind} \`${type}\` is registered in the runtime but no docs page lists it in octo_types`);
  }
}
for (const [type, files] of documented) {
  if (!registered.has(type) && !allowlist.unregistered?.includes(type)) {
    failures.push(`UNREGISTERED  \`${type}\` is documented in ${files.join(', ')} but not registered in the runtime`);
  }
  if (files.length > 1) {
    failures.push(`DUPLICATE     \`${type}\` is claimed by multiple pages: ${files.join(', ')}`);
  }
}

if (failures.length > 0) {
  console.error('docs drift check FAILED:\n');
  for (const f of failures) console.error(`  ${f}`);
  console.error(`\nFix: update the octo_types frontmatter of the owning page under apps/docs/content/docs/ (see AGENTS.md "Documentation policy").`);
  process.exit(1);
}

console.log(`docs drift check passed: ${registered.size} registered types, all documented exactly once.`);
