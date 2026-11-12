#!/usr/bin/env node
// Fails CI when a flow someone might copy breaks the mechanical flow-authoring
// rules, or when the three places that teach those rules stop agreeing.
//
// Source of truth: apps/docs/content/docs/guides/flow-best-practices.mdx. Each
// rule there is a heading carrying its id (`## ... [#multi-transform]`).
//
// What is checked:
//   1. Flows: samples/ (and its folders), Dr. Octo (orchestrator/agent) and the
//      MCP examples (packages/mcp/src/examples.ts). Test files are skipped.
//        multi-transform  no two consecutive set-variable/set-payload siblings
//        name-blocks      every block has a name (a `ref:` step inherits one)
//   2. Rule drift: every rule id in the doc is taught by the MCP prompt
//      (packages/mcp/src/prompts.ts, as `[id]`) and by Dr. Octo's
//      best-practices skill (as a `## id —` section).
//
// A deliberate exception is a comment directly above the block:
//   # practices: allow multi-transform
//
// Usage: node scripts/check-flow-practices.mjs

import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const { parseDocument, isMap, isSeq, isScalar } = createRequire(join(repo, 'packages', 'mcp', 'package.json'))('yaml');

const guide = join(repo, 'apps', 'docs', 'content', 'docs', 'guides', 'flow-best-practices.mdx');
const mcpPrompt = join(repo, 'packages', 'mcp', 'src', 'prompts.ts');
const mcpExamples = join(repo, 'packages', 'mcp', 'src', 'examples.ts');
const skill = join(repo, 'orchestrator', 'agent', 'skills', 'best-practices.md');

const SETTERS = new Set(['set-variable', 'set-payload']);
// Keys whose value is a chain of blocks: a flow's process and error, and every
// composite slot (then/else/body/default/cases[]/...), which nests one as process.
const CHAIN_KEYS = new Set(['process', 'error']);

const problems = [];

// ---------------------------------------------------------------- 1. flows

/** Every flow file under dir: *.yaml at any depth, minus suites. */
function flowFiles(dir) {
  const out = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      out.push(...flowFiles(path));
    } else if (/\.ya?ml$/.test(entry) && !/_test\.ya?ml$/.test(entry)) {
      out.push(path);
    }
  }
  return out;
}

/** The YAML documents of examples.ts: each `definition:` template literal. */
function exampleDefinitions() {
  const src = readFileSync(mcpExamples, 'utf8');
  const out = [];
  for (const m of src.matchAll(/slug: "([^"]+)"[\s\S]*?definition: `((?:\\[\s\S]|[^`\\])*)`/g)) {
    // Undo the template literal's escapes; ${...} never appears unescaped.
    const text = m[2].replace(/\\([`$\\])/g, '$1');
    // The definition's first line shares the `definition:` line, so its line
    // numbers are offset by the lines before that one. Escapes span no newlines.
    const start = m.index + m[0].indexOf('definition: `');
    const base = src.slice(0, start).split('\n').length - 1;
    out.push({ where: `${relative(repo, mcpExamples)} (${m[1]})`, text, base });
  }
  return out;
}

function allowed(node, rule) {
  return (node.commentBefore ?? '').includes(`practices: allow ${rule}`);
}

function get(map, key) {
  const pair = map.items.find((p) => isScalar(p.key) && p.key.value === key);
  return pair ? pair.value : undefined;
}

/** Walk any node, checking every chain of blocks found beneath it. */
function walk(node, where, path) {
  if (isMap(node)) {
    for (const pair of node.items) {
      const key = isScalar(pair.key) ? String(pair.key.value) : '?';
      if (CHAIN_KEYS.has(key) && isSeq(pair.value)) {
        checkChain(pair.value, where, `${path}.${key}`);
      }
      walk(pair.value, where, `${path}.${key}`);
    }
  } else if (isSeq(node)) {
    node.items.forEach((item, i) => {
      const name = isMap(item) && get(item, 'name');
      walk(item, where, `${path}[${name && isScalar(name) ? name.value : i}]`);
    });
  }
}

function checkChain(seq, where, path) {
  let previous = null;
  for (const block of seq.items) {
    if (!isMap(block)) continue;
    const type = get(block, 'type');
    const typeName = type && isScalar(type) ? String(type.value) : null;
    if (!typeName) continue; // not a block (a ref-only step is still one; see below)
    const name = get(block, 'name');
    // A raw-content set-payload has no multi-transform equivalent: setBody has no
    // rawBody, so it ends a run of setters rather than joining one.
    const settings = get(block, 'settings');
    const raw = isMap(settings) && isScalar(get(settings, 'rawBody')) && get(settings, 'rawBody').value === true;
    const setter = SETTERS.has(typeName) && !raw;
    const label = name && isScalar(name) ? name.value : `<${typeName}>`;
    const line = lineOf(where, block);

    if (!name && !get(block, 'ref') && !allowed(block, 'name-blocks')) {
      problems.push(`${where}:${line}  ${path}: a ${typeName} block has no name  [name-blocks]`);
    }
    if (setter && previous && !allowed(block, 'multi-transform')) {
      problems.push(
        `${where}:${line}  ${path}: ${previous} then ${label} — fold consecutive setters into one multi-transform  [multi-transform]`,
      );
    }
    previous = setter ? label : null;
  }
}

const sources = new Map(); // where -> { text, base }, for line numbers
function lineOf(where, node) {
  const source = sources.get(where);
  if (!source || !node.range) return '?';
  return source.base + source.text.slice(0, node.range[0]).split('\n').length;
}

function checkFlowText(where, text, base = 0) {
  sources.set(where, { text, base });
  const doc = parseDocument(text);
  if (doc.errors.length > 0) {
    problems.push(`${where}: does not parse: ${doc.errors[0].message}`);
    return;
  }
  const flows = isMap(doc.contents) && get(doc.contents, 'flows');
  if (flows) walk(flows, where, 'flows');
  const processors = isMap(doc.contents) && get(doc.contents, 'processors');
  if (processors && isSeq(processors)) checkChain(processors, where, 'processors');
}

const flowDirs = [join(repo, 'samples'), join(repo, 'orchestrator', 'agent')];
let checked = 0;
for (const dir of flowDirs) {
  for (const file of flowFiles(dir)) {
    const text = readFileSync(file, 'utf8');
    if (!/^flows:/m.test(text) && !/^processors:/m.test(text)) continue; // data, not a flow file
    checkFlowText(relative(repo, file), text);
    checked++;
  }
}
for (const { where, text, base } of exampleDefinitions()) {
  checkFlowText(where, text, base);
  checked++;
}
if (checked === 0) {
  console.error('no flow files found — refusing to pass vacuously');
  process.exit(2);
}

// ---------------------------------------------------------- 2. rule drift

const read = (path) => (existsSync(path) ? readFileSync(path, 'utf8') : null);
const ruleIds = [...(read(guide) ?? '').matchAll(/^##+ .*\[#([a-z0-9-]+)\]\s*$/gm)].map((m) => m[1]);
if (ruleIds.length === 0) {
  problems.push(`${relative(repo, guide)}: missing, or declares no rule ids (## Heading [#id])  [drift]`);
}
// Each source states a rule in its own shape: the prompt tags it `[id]`, the skill
// opens a section with `## id —`. Matching the shape, not the bare word, keeps a
// rule whose id is also a block name (multi-transform) from passing on a mention.
const taught = [
  { path: mcpPrompt, shape: (id) => `[${id}]`, find: (text, id) => text.includes(`[${id}]`) },
  { path: skill, shape: (id) => `## ${id} —`, find: (text, id) => new RegExp(`^## ${id} —`, 'm').test(text) },
];
for (const { path, shape, find } of taught) {
  const text = read(path);
  if (text === null) {
    problems.push(`${relative(repo, path)}: missing  [drift]`);
    continue;
  }
  for (const id of ruleIds) {
    if (!find(text, id)) {
      problems.push(`${relative(repo, path)}: does not teach rule \`${id}\` from the best-practices guide (expected "${shape(id)}")  [drift]`);
    }
  }
}

// ---------------------------------------------------------------- report

if (problems.length > 0) {
  console.error(`flow practices: ${problems.length} problem(s)\n`);
  for (const p of problems) console.error(`  ${p}`);
  console.error(`\nSee ${relative(repo, guide)}.`);
  process.exit(1);
}
console.log(`flow practices: ${checked} flow files and ${ruleIds.length} rules check out`);
