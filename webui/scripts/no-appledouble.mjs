#!/usr/bin/env node

import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webuiRoot = path.resolve(scriptDir, '..');
const excludedDirs = new Set([
  '.git',
  'node_modules',
  'dist',
  '.vite',
  '.turbo',
  'coverage'
]);

async function walk(dir, hits = []) {
  const entries = await fs.readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    const abs = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (excludedDirs.has(entry.name)) continue;
      await walk(abs, hits);
      continue;
    }
    if (!entry.isFile()) continue;
    if (entry.name.startsWith('._')) {
      hits.push(path.relative(webuiRoot, abs));
    }
  }
  return hits;
}

async function main() {
  const hits = await walk(webuiRoot);
  if (hits.length > 0) {
    console.error('❌ AppleDouble artifacts detected under webui/.\n');
    for (const hit of hits) {
      console.error(`- ${hit}`);
    }
    console.error('\nFix: remove files named "._*" before running tests.');
    process.exit(1);
  }

  console.log('✅ AppleDouble gate passed: no ._* artifacts under webui/.');
}

main().catch((err) => {
  console.error('❌ AppleDouble gate failed due to execution error.');
  console.error(err instanceof Error ? err.message : String(err));
  process.exit(1);
});
