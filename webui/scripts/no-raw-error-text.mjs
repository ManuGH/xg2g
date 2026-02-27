#!/usr/bin/env node

import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webuiRoot = path.resolve(scriptDir, '..');
const srcRoot = path.join(webuiRoot, 'src');
const allowTag = 'raw-error-justified:';
const listAllowed = process.argv.includes('--list-allowed');

const includeExt = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs']);
const excludedFilePattern = [/\.test\./, /\.spec\./, /\.stories\./, /__tests__/, /\/client-ts\//];

const directThrowTextRe = /throw\s+new\s+Error\s*\(\s*await\s+[^;]*?\.text\s*\(/;
const directThrowJsonRe = /throw\s+new\s+Error\s*\(\s*await\s+[^;]*?\.json\s*\(/;
const directSetErrorTextRe = /setError\s*\(\s*await\s+[^;]*?\.text\s*\(/;
const directSetErrorJsonRe = /setError\s*\(\s*await\s+[^;]*?\.json\s*\(/;
const awaitedTextRe = /await\s+[^;]*?\.text\s*\(/;

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

function hasAllowTag(lines, lineIndex) {
  const current = lines[lineIndex] ?? '';
  const previous = lineIndex > 0 ? lines[lineIndex - 1] : '';
  return current.includes(allowTag) || previous.includes(allowTag);
}

function scanFile(filePath, source) {
  const rel = path.relative(webuiRoot, filePath);
  const lines = source.split(/\r?\n/);
  const violations = [];
  const allowlisted = [];
  let totalHits = 0;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const snippet = line.trim();
    const allowed = hasAllowTag(lines, i);

    const directMatch =
      directThrowTextRe.test(line) ||
      directThrowJsonRe.test(line) ||
      directSetErrorTextRe.test(line) ||
      directSetErrorJsonRe.test(line);

    if (directMatch) {
      totalHits += 1;
      if (allowed) {
        allowlisted.push({ file: rel, line: i + 1, snippet });
      } else {
        violations.push({
          file: rel,
          line: i + 1,
          snippet,
          reason: 'Direct throw/setError from raw response body is forbidden. Use RFC7807 helper parsing.'
        });
      }
      continue;
    }

    if (!awaitedTextRe.test(line)) continue;

    const windowEnd = Math.min(lines.length, i + 10);
    const windowText = lines.slice(i, windowEnd).join('\n');
    const textEscalates = /throw\s+new\s+Error\s*\(|\bsetError\s*\(/.test(windowText);
    if (!textEscalates) continue;

    totalHits += 1;
    if (allowed) {
      allowlisted.push({ file: rel, line: i + 1, snippet });
      continue;
    }
    violations.push({
      file: rel,
      line: i + 1,
      snippet,
      reason: 'Response text consumed near throw/setError path. Parse problem+json centrally instead.'
    });
  }

  return { violations, allowlisted, totalHits };
}

async function main() {
  const files = await collectFiles(srcRoot);
  const violations = [];
  const allowlisted = [];
  let totalHits = 0;

  for (const file of files) {
    const source = await fs.readFile(file, 'utf8');
    const result = scanFile(file, source);
    violations.push(...result.violations);
    allowlisted.push(...result.allowlisted);
    totalHits += result.totalHits;
  }

  if (violations.length > 0) {
    console.error('❌ Gate N failed: non-RFC7807 error handling detected.\n');
    for (const v of violations) {
      console.error(`- ${v.file}:${v.line}`);
      console.error(`  reason: ${v.reason}`);
      console.error(`  code:   ${v.snippet}`);
      console.error(`  fix:    use httpProblem helper or add '${allowTag} <reason>'`);
    }
    console.error('\nSummary');
    console.error(`- scanned_files: ${files.length}`);
    console.error(`- hits: ${totalHits}`);
    console.error(`- allowlisted: ${allowlisted.length}`);
    console.error(`- violations: ${violations.length}`);
    process.exit(1);
  }

  console.log('✅ Gate N passed: RFC7807-only raw error handling.');
  console.log(`- scanned_files: ${files.length}`);
  console.log(`- hits: ${totalHits}`);
  console.log(`- allowlisted: ${allowlisted.length}`);
  console.log('- violations: 0');

  if (listAllowed && allowlisted.length > 0) {
    console.log('\nAllowlisted raw error paths:');
    for (const item of allowlisted) {
      console.log(`- ${item.file}:${item.line} :: ${item.snippet}`);
    }
  }
}

main().catch((err) => {
  console.error('❌ Gate N failed due to execution error.');
  console.error(err instanceof Error ? err.message : String(err));
  process.exit(1);
});
