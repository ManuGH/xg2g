#!/usr/bin/env node

import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webuiRoot = path.resolve(scriptDir, '..');
const srcRoot = path.join(webuiRoot, 'src');
const allowTag = 'ua-telemetry-only';
const listAllowed = process.argv.includes('--list-allowed');

const includeExt = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs']);
const excludedFilePattern = [/\.test\./, /\.spec\./, /\.stories\./, /__tests__/, /\/client-ts\//];

const uaAccessRe =
  /\b(?:navigator|window\s*\.\s*navigator)\s*\.\s*(?:userAgent|platform|vendor|appVersion)\b/;

async function collectFiles(dir, out = []) {
  const entries = await fs.readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    const abs = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      await collectFiles(abs, out);
      continue;
    }
    if (!entry.isFile()) continue;
    if (!includeExt.has(path.extname(entry.name))) continue;
    if (excludedFilePattern.some((rx) => rx.test(abs))) continue;
    out.push(abs);
  }
  return out;
}

function stripLineComment(line) {
  return line.replace(/\/\/.*$/, '');
}

function hasAllowTag(lines, idx) {
  const current = lines[idx] ?? '';
  const previous = idx > 0 ? lines[idx - 1] : '';
  return current.includes(allowTag) || previous.includes(allowTag);
}

function scanFile(filePath, source) {
  const rel = path.relative(webuiRoot, filePath);
  const lines = source.split(/\r?\n/);
  const violations = [];
  const allowed = [];
  let hits = 0;

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i] ?? '';
    const code = stripLineComment(raw);
    if (!uaAccessRe.test(code)) continue;

    hits += 1;
    const snippet = raw.trim();
    if (hasAllowTag(lines, i)) {
      allowed.push({
        file: rel,
        line: i + 1,
        snippet
      });
      continue;
    }

    violations.push({
      file: rel,
      line: i + 1,
      snippet,
      reason: 'UA sniffing in runtime WebUI logic is forbidden; use capability detection and backend mode.'
    });
  }

  return { hits, allowed, violations };
}

async function main() {
  const files = await collectFiles(srcRoot);
  const violations = [];
  const allowed = [];
  let hits = 0;

  for (const file of files) {
    const source = await fs.readFile(file, 'utf8');
    const result = scanFile(file, source);
    hits += result.hits;
    allowed.push(...result.allowed);
    violations.push(...result.violations);
  }

  if (violations.length > 0) {
    console.error('❌ Gate W failed: UA-sniffing patterns detected.\n');
    for (const v of violations) {
      console.error(`- ${v.file}:${v.line}`);
      console.error(`  reason: ${v.reason}`);
      console.error(`  code:   ${v.snippet}`);
      console.error(`  fix:    remove UA-based logic or mark telemetry usage with '${allowTag}'`);
    }
    console.error('\nSummary');
    console.error(`- scanned_files: ${files.length}`);
    console.error(`- hits: ${hits}`);
    console.error(`- allowlisted: ${allowed.length}`);
    console.error(`- violations: ${violations.length}`);
    process.exit(1);
  }

  console.log('✅ Gate W passed: no UA-sniffing in runtime WebUI logic.');
  console.log(`- scanned_files: ${files.length}`);
  console.log(`- hits: ${hits}`);
  console.log(`- allowlisted: ${allowed.length}`);
  console.log('- violations: 0');

  if (listAllowed && allowed.length > 0) {
    console.log('\nAllowlisted UA accesses:');
    for (const entry of allowed) {
      console.log(`- ${entry.file}:${entry.line} :: ${entry.snippet}`);
    }
  }
}

main().catch((err) => {
  console.error('❌ Gate W failed due to execution error.');
  console.error(err instanceof Error ? err.message : String(err));
  process.exit(1);
});
