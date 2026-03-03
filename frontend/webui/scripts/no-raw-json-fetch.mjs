#!/usr/bin/env node

import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webuiRoot = path.resolve(scriptDir, '..');
const srcRoot = path.join(webuiRoot, 'src');
const allowTag = 'raw-fetch-justified:';
const listAllowed = process.argv.includes('--list-allowed');

const includeExt = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs']);
const excludedFilePattern = [/\.test\./, /\.spec\./, /\.stories\./, /__tests__/];

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

function buildFetchCandidates(lines) {
  const candidates = [];
  const assignmentRe = /\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*await\s+fetch\s*\(/;
  const fetchRe = /\bfetch\s*\(/;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (!fetchRe.test(line)) continue;

    const assignment = line.match(assignmentRe);
    candidates.push({
      line: i + 1,
      lineIndex: i,
      varName: assignment ? assignment[1] : null,
    });
  }
  return candidates;
}

function findFetchCallEnd(lines, startIndex) {
  let parenDepth = 0;
  let seenOpen = false;
  let inSingle = false;
  let inDouble = false;
  let inBacktick = false;
  let escaped = false;

  for (let i = startIndex; i < lines.length; i++) {
    const line = lines[i];
    for (let j = 0; j < line.length; j++) {
      const ch = line[j];
      if (escaped) {
        escaped = false;
        continue;
      }

      if (ch === '\\') {
        escaped = true;
        continue;
      }

      if (!inDouble && !inBacktick && ch === "'") {
        inSingle = !inSingle;
        continue;
      }
      if (!inSingle && !inBacktick && ch === '"') {
        inDouble = !inDouble;
        continue;
      }
      if (!inSingle && !inDouble && ch === '`') {
        inBacktick = !inBacktick;
        continue;
      }
      if (inSingle || inDouble || inBacktick) continue;

      if (ch === '(') {
        parenDepth += 1;
        seenOpen = true;
      } else if (ch === ')') {
        parenDepth -= 1;
        if (seenOpen && parenDepth <= 0) {
          return i;
        }
      }
    }
  }

  return Math.min(lines.length - 1, startIndex + 12);
}

function isApiLikeFetch(fetchCallText) {
  return (
    /\bapiBase\b/.test(fetchCallText) ||
    /\/api\//.test(fetchCallText) ||
    /\/internal\//.test(fetchCallText)
  );
}

function isJsonFetch(fetchCallText, followupText, varName) {
  if (/application\/json/i.test(fetchCallText)) return true;
  if (/\bJSON\.stringify\s*\(/.test(fetchCallText)) return true;
  if (varName) {
    const varJsonRe = new RegExp(`\\b${varName}\\s*\\.\\s*json\\s*\\(`, 'm');
    if (varJsonRe.test(followupText)) return true;
  }
  return false;
}

function hasAllowTag(lines, lineIndex) {
  const current = lines[lineIndex] ?? '';
  const previous = lineIndex > 0 ? lines[lineIndex - 1] : '';
  return current.includes(allowTag) || previous.includes(allowTag);
}

function scanFile(filePath, text) {
  const rel = path.relative(webuiRoot, filePath);
  const lines = text.split(/\r?\n/);
  const candidates = buildFetchCandidates(lines);
  const violations = [];
  const allowed = [];

  for (const candidate of candidates) {
    const start = candidate.lineIndex;
    const fetchEnd = findFetchCallEnd(lines, start);
    const callText = lines.slice(start, fetchEnd + 1).join('\n');
    const followupEnd = Math.min(lines.length, fetchEnd + 41);
    const followupText = lines.slice(fetchEnd + 1, followupEnd).join('\n');

    if (!isApiLikeFetch(callText)) continue;
    if (!isJsonFetch(callText, followupText, candidate.varName)) continue;

    const snippet = (lines[start] ?? '').trim();
    if (hasAllowTag(lines, start)) {
      allowed.push({
        file: rel,
        line: candidate.line,
        snippet,
      });
      continue;
    }

    violations.push({
      file: rel,
      line: candidate.line,
      snippet,
      reason: 'Raw JSON fetch to API/internal endpoint without explicit justification tag.',
    });
  }

  return { violations, allowed, totalCandidates: candidates.length };
}

async function main() {
  const files = await collectFiles(srcRoot);
  const violations = [];
  const allowed = [];
  let totalCandidates = 0;

  for (const file of files) {
    const text = await fs.readFile(file, 'utf8');
    const result = scanFile(file, text);
    violations.push(...result.violations);
    allowed.push(...result.allowed);
    totalCandidates += result.totalCandidates;
  }

  if (violations.length > 0) {
    console.error('❌ Gate M failed: hidden raw JSON fetch detected.\n');
    for (const v of violations) {
      console.error(`- ${v.file}:${v.line}`);
      console.error(`  reason: ${v.reason}`);
      console.error(`  code:   ${v.snippet}`);
      console.error(`  fix:    add '${allowTag} <reason>' or migrate to client-ts SDK`);
    }
    console.error('\nSummary');
    console.error(`- scanned_files: ${files.length}`);
    console.error(`- fetch_calls_seen: ${totalCandidates}`);
    console.error(`- allowlisted: ${allowed.length}`);
    console.error(`- violations: ${violations.length}`);
    process.exit(1);
  }

  console.log('✅ Gate M passed: no hidden raw JSON fetch.');
  console.log(`- scanned_files: ${files.length}`);
  console.log(`- fetch_calls_seen: ${totalCandidates}`);
  console.log(`- allowlisted: ${allowed.length}`);
  console.log('- violations: 0');

  if (listAllowed && allowed.length > 0) {
    console.log('\nAllowlisted raw fetch calls:');
    for (const entry of allowed) {
      console.log(`- ${entry.file}:${entry.line} :: ${entry.snippet}`);
    }
  }
}

main().catch((err) => {
  console.error('❌ Gate M failed due to execution error.');
  console.error(err instanceof Error ? err.message : String(err));
  process.exit(1);
});
