#!/usr/bin/env node

import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webuiRoot = path.resolve(scriptDir, '..');
const srcRoot = path.join(webuiRoot, 'src');
const allowTag = 'duration-display-only';

const includeExt = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs']);
const excludedFilePattern = [/\.test\./, /\.spec\./, /\.stories\./, /__tests__/];

const checks = [
  {
    id: 'duration_property_access',
    reason: 'UI must not use *.duration as playback truth input. Use backend durationMs/isSeekable DTO.',
    regex: /\.\s*duration\b/g,
    allowlisted: true,
  },
  {
    id: 'm3u8_segment_parsing',
    reason: 'UI must not parse playlist segments for duration truth.',
    regex: /#EXTM3U|#EXTINF|#EXT-X-(?:TARGETDURATION|PROGRAM-DATE-TIME|ENDLIST|PLAYLIST-TYPE)/g,
    allowlisted: false,
  },
];

async function collectFiles(dir, out = []) {
  const entries = await fs.readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    const abs = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      await collectFiles(abs, out);
      continue;
    }
    if (!entry.isFile()) continue;
    const ext = path.extname(entry.name);
    if (!includeExt.has(ext)) continue;
    if (excludedFilePattern.some((rx) => rx.test(abs))) continue;
    out.push(abs);
  }
  return out;
}

function hasAllowTag(line, prevLine) {
  return line.includes(allowTag) || prevLine.includes(allowTag);
}

function collectLineViolations(filePath, text) {
  const rel = path.relative(webuiRoot, filePath);
  const lines = text.split(/\r?\n/);
  const violations = [];
  const allowlistedHits = [];
  let totalHits = 0;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const prevLine = i > 0 ? lines[i - 1] : '';
    for (const check of checks) {
      const matches = [...line.matchAll(check.regex)];
      if (matches.length === 0) continue;
      totalHits += matches.length;

      for (const m of matches) {
        const col = (m.index ?? 0) + 1;
        if (check.allowlisted && hasAllowTag(line, prevLine)) {
          allowlistedHits.push({
            check: check.id,
            file: rel,
            line: i + 1,
            col,
          });
          continue;
        }

        violations.push({
          check: check.id,
          reason: check.reason,
          file: rel,
          line: i + 1,
          col,
          snippet: line.trim(),
        });
      }
    }
  }

  return { violations, allowlistedHits, totalHits };
}

async function main() {
  const files = await collectFiles(srcRoot);
  let totalHits = 0;
  const allViolations = [];
  const allAllowlisted = [];

  for (const file of files) {
    const src = await fs.readFile(file, 'utf8');
    const result = collectLineViolations(file, src);
    totalHits += result.totalHits;
    allViolations.push(...result.violations);
    allAllowlisted.push(...result.allowlistedHits);
  }

  if (allViolations.length > 0) {
    console.error('❌ Gate K failed: duration guessing patterns detected.\n');
    for (const v of allViolations) {
      console.error(`- [${v.check}] ${v.file}:${v.line}:${v.col}`);
      console.error(`  reason: ${v.reason}`);
      console.error(`  code:   ${v.snippet}`);
    }
    console.error('\nSummary');
    console.error(`- scanned_files: ${files.length}`);
    console.error(`- total_hits: ${totalHits}`);
    console.error(`- allowlisted_hits: ${allAllowlisted.length}`);
    console.error(`- violations: ${allViolations.length}`);
    process.exit(1);
  }

  console.log('✅ Gate K passed: no duration guessing violations.');
  console.log(`- scanned_files: ${files.length}`);
  console.log(`- total_hits: ${totalHits}`);
  console.log(`- allowlisted_hits: ${allAllowlisted.length}`);
  console.log('- violations: 0');
}

main().catch((err) => {
  console.error('❌ Gate K failed due to execution error.');
  console.error(err instanceof Error ? err.message : String(err));
  process.exit(1);
});
