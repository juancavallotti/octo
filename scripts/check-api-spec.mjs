#!/usr/bin/env node
// Fails CI when the public API's description drifts from its routes.
//
// Source of truth: the http sources of the flows under api/ — every flow fronted
// by one is a route the API serves. Coverage claim: each route (path + method)
// is described in api/static/openapi.yaml, and each operation described there
// is still served. The spec is written by hand, so this is the only thing that
// notices a route added or removed without it.
//
// Usage: node scripts/check-api-spec.mjs  (needs `pnpm install` for `yaml`)

import { readFileSync, readdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const apiDir = join(repo, 'api');
const specPath = join(apiDir, 'static', 'openapi.yaml');

// The workspace has no root dependency on yaml; borrow the one a package already
// installs rather than add one for a script.
const { parse } = createRequire(join(repo, 'packages', 'mcp', 'package.json'))('yaml');

const httpMethods = ['get', 'put', 'post', 'delete', 'patch', 'head', 'options'];

// 1. The routes the flows serve. A source with no methods answers every one,
//    which a described API should never do, so it is reported rather than guessed.
const served = new Set();
const problems = [];
for (const file of readdirSync(apiDir).filter((f) => f.endsWith('.yaml') && !f.endsWith('_test.yaml'))) {
  const doc = parse(readFileSync(join(apiDir, file), 'utf8')) ?? {};
  for (const flow of doc.flows ?? []) {
    const source = flow.source;
    if (!source || source.type !== 'http') continue;
    const { path, methods } = source.settings ?? {};
    if (!methods?.length) {
      problems.push(`${file}: flow ${flow.name} serves ${path} without naming its methods`);
      continue;
    }
    for (const method of methods) served.add(`${method.toUpperCase()} ${path}`);
  }
}

// 2. The operations the spec describes.
const spec = parse(readFileSync(specPath, 'utf8'));
const described = new Set();
for (const [path, item] of Object.entries(spec.paths ?? {})) {
  for (const method of httpMethods) {
    if (item[method]) described.add(`${method.toUpperCase()} ${path}`);
  }
}

if (served.size === 0) {
  console.error('no http routes found under api/ — refusing to pass vacuously');
  process.exit(2);
}

for (const route of [...served].sort()) {
  if (!described.has(route)) problems.push(`served but not described: ${route}`);
}
for (const route of [...described].sort()) {
  if (!served.has(route)) problems.push(`described but not served: ${route}`);
}

if (problems.length > 0) {
  console.error('public API spec drift:');
  for (const p of problems) console.error(`  - ${p}`);
  console.error(`\nUpdate ${specPath.replace(repo + '/', '')} alongside the flows.`);
  process.exit(1);
}
console.log(`public API spec check passed: ${served.size} routes, all described exactly as served.`);
