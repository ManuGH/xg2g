#!/usr/bin/env node

import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webuiRoot = path.resolve(scriptDir, '..');
const srcRoot = path.join(webuiRoot, 'src');
const listAllowed = process.argv.includes('--list-allowed');

const allowTags = ['visual-clamp-only', 'display-only'];
const includeExt = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs']);
const excludedFilePattern = [/\.test\./, /\.spec\./, /\.stories\./, /__tests__/, /\/client-ts\//];

const clampCallRe = /\bMath\.(?:min|max)\s*\(|\bclamp\s*\(/;
const clampDeclRe = /^\s*(?:export\s+)?function\s+clamp\s*\(/;
const seekResumeContextRe =
  /\b(?:seek|seekable|canSeek|isSeekable|resume|posSeconds|position|currentPlaybackTime|duration(?:Ms|Seconds)?)\b/i;
const derivedSeekableRe =
  /\b(?:isSeekable|canSeek|seekable)\b\s*(?:=|:)\s*[^;\n]*\bduration(?:Ms|Seconds)?\b/i;
const derivedSeekableSetFnRe =
  /\bset(?:CanSeek|IsSeekable)\s*\(\s*[^)\n]*\bduration(?:Ms|Seconds)?\b/i;
const posModuloDurationRe =
  /\b(?:pos|position|seek|resume)[A-Za-z0-9_]*\b\s*=\s*[^;\n%]*%\s*[^;\n]*\bduration(?:Ms|Seconds)?\b/i;

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
  return allowTags.some((tag) => current.includes(tag) || previous.includes(tag));
}

function pushViolation(list, rel, line, col, check, reason, snippet) {
  list.push({
    file: rel,
    line,
    col,
    check,
    reason,
    snippet: snippet.trim()
  });
}

function pushAllowed(list, rel, line, check, snippet) {
  list.push({
    file: rel,
    line,
    check,
    snippet: snippet.trim()
  });
}

function scanFile(filePath, source) {
  const rel = path.relative(webuiRoot, filePath);
  const lines = source.split(/\r?\n/);
  const violations = [];
  const allowed = [];
  let hits = 0;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] ?? '';

    if (clampDeclRe.test(line)) {
      continue;
    }

    if (clampCallRe.test(line)) {
      const windowStart = Math.max(0, i - 2);
      const windowEnd = Math.min(lines.length, i + 3);
      const windowText = lines.slice(windowStart, windowEnd).join('\n');
      if (seekResumeContextRe.test(windowText)) {
        hits += 1;
        if (hasAllowTag(lines, i)) {
          pushAllowed(allowed, rel, i + 1, 'seek_resume_clamp', line);
        } else {
          pushViolation(
            violations,
            rel,
            i + 1,
            1,
            'seek_resume_clamp',
            'Seek/resume clamp logic detected. Use server truth or mark visual-only clamp explicitly.',
            line
          );
        }
      }
    }

    if (derivedSeekableRe.test(line) || derivedSeekableSetFnRe.test(line)) {
      if (!/\b(?:isSeekable|canSeek|seekable|setCanSeek|setIsSeekable)\b/.test(line)) {
        continue;
      }
      hits += 1;
      if (hasAllowTag(lines, i)) {
        pushAllowed(allowed, rel, i + 1, 'derived_seekable', line);
      } else {
        pushViolation(
          violations,
          rel,
          i + 1,
          1,
          'derived_seekable',
          'Seekability appears derived from duration/client logic. Seekability must come from server DTO.',
          line
        );
      }
    }

    if (posModuloDurationRe.test(line)) {
      hits += 1;
      if (hasAllowTag(lines, i)) {
        pushAllowed(allowed, rel, i + 1, 'pos_normalization', line);
      } else {
        pushViolation(
          violations,
          rel,
          i + 1,
          1,
          'pos_normalization',
          'Position normalization into duration range is forbidden in UI policy path.',
          line
        );
      }
    }
  }

  return { violations, allowed, hits };
}

async function main() {
  const files = await collectFiles(srcRoot);
  const violations = [];
  const allowed = [];
  let totalHits = 0;

  for (const file of files) {
    const source = await fs.readFile(file, 'utf8');
    const result = scanFile(file, source);
    violations.push(...result.violations);
    allowed.push(...result.allowed);
    totalHits += result.hits;
  }

  if (violations.length > 0) {
    console.error('❌ Gate O failed: seek/resume guessing patterns detected.\n');
    for (const v of violations) {
      console.error(`- [${v.check}] ${v.file}:${v.line}:${v.col}`);
      console.error(`  reason: ${v.reason}`);
      console.error(`  code:   ${v.snippet}`);
      console.error('  fix:    move policy to backend or annotate purely visual clamps with allow tag');
    }
    console.error('\nSummary');
    console.error(`- scanned_files: ${files.length}`);
    console.error(`- hits: ${totalHits}`);
    console.error(`- allowlisted: ${allowed.length}`);
    console.error(`- violations: ${violations.length}`);
    process.exit(1);
  }

  console.log('✅ Gate O passed: no seek/resume guessing violations.');
  console.log(`- scanned_files: ${files.length}`);
  console.log(`- hits: ${totalHits}`);
  console.log(`- allowlisted: ${allowed.length}`);
  console.log('- violations: 0');

  if (listAllowed && allowed.length > 0) {
    console.log('\nAllowlisted lines:');
    for (const entry of allowed) {
      console.log(`- [${entry.check}] ${entry.file}:${entry.line} :: ${entry.snippet}`);
    }
  }
}

main().catch((err) => {
  console.error('❌ Gate O failed due to execution error.');
  console.error(err instanceof Error ? err.message : String(err));
  process.exit(1);
});
