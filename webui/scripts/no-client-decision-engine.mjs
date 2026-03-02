#!/usr/bin/env node

import { promises as fs } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webuiRoot = path.resolve(scriptDir, '..');
const srcRoot = path.join(webuiRoot, 'src');
const listAllowed = process.argv.includes('--list-allowed');

const clientBridgeTag = 'client-bridge-only';
const displayOnlyTag = 'display-only';

const includeExt = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs']);
const excludedFilePattern = [/\.test\./, /\.spec\./, /\.stories\./, /__tests__/, /\/client-ts\//];
const bridgeAllowlist = new Set([path.join(srcRoot, 'components', 'v3playerModeBridge.ts')]);

const branchSignalRe = /\bif\s*\(|\bswitch\s*\(|\?[^:\n]{0,160}:/;
const strongDecisionWordRe = /\b(?:directplay|directstream|decisionReason|playbackDecision|policy)\b/i;
const transcodeWordRe = /\btranscode\b/i;
const transcodePolicyContextRe = /\b(?:reason|decisionReason|playbackDecision|policy|capability|capabilities|supportsHls|allowTranscode)\b/i;
const modeFallbackRe = /\b(?:mode|playbackMode|liveMode|vodMode)\s*(?:\?\?|\|\|)\s*['"][^'"]+['"]/;
const defaultModeReturnRe = /\bdefault\s*:\s*return\s*['"](?:hlsjs|native_hls|direct_mp4|transcode|deny)['"]/;
const supportsHlsOverrideRe = /\bsupportsHls\s*:\s*true\b/;

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

function hasAllowTag(lines, lineIndex) {
  const current = lines[lineIndex] ?? '';
  const previous = lineIndex > 0 ? lines[lineIndex - 1] : '';
  return (
    current.includes(displayOnlyTag) ||
    previous.includes(displayOnlyTag) ||
    current.includes(clientBridgeTag) ||
    previous.includes(clientBridgeTag)
  );
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
  const isBridgeFile = bridgeAllowlist.has(filePath);
  let hits = 0;

  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i] ?? '';
    const codeLine = stripLineComment(rawLine);

    if (rawLine.includes(clientBridgeTag) && !isBridgeFile) {
      hits += 1;
      pushViolation(
        violations,
        rel,
        i + 1,
        rawLine.indexOf(clientBridgeTag) + 1,
        'bridge_tag_outside_allowlist',
        '`client-bridge-only` is only allowed in bridge SSOT files.',
        rawLine
      );
      continue;
    }

    if (isBridgeFile) {
      if (rawLine.includes(clientBridgeTag) || rawLine.includes(displayOnlyTag)) {
        hits += 1;
        pushAllowed(allowed, rel, i + 1, 'bridge_allowlist_tag', rawLine);
      }
      continue;
    }

    if (modeFallbackRe.test(codeLine) || defaultModeReturnRe.test(codeLine)) {
      hits += 1;
      if (hasAllowTag(lines, i)) {
        pushAllowed(allowed, rel, i + 1, 'mode_fallback', rawLine);
      } else {
        pushViolation(
          violations,
          rel,
          i + 1,
          1,
          'mode_fallback',
          'Mode fallback (`??`, `||`, or default return) is forbidden outside mode bridge SSOT.',
          rawLine
        );
      }
    }

    if (supportsHlsOverrideRe.test(codeLine)) {
      hits += 1;
      if (hasAllowTag(lines, i)) {
        pushAllowed(allowed, rel, i + 1, 'supports_hls_override', rawLine);
      } else {
        pushViolation(
          violations,
          rel,
          i + 1,
          1,
          'supports_hls_override',
          'Hard-coded `supportsHls: true` is forbidden outside feature detection utilities.',
          rawLine
        );
      }
    }

    if (!branchSignalRe.test(codeLine)) continue;

    const start = Math.max(0, i - 2);
    const end = Math.min(lines.length, i + 3);
    const windowCode = lines.slice(start, end).map(stripLineComment).join('\n');
    const hasStrongDecisionSignal = strongDecisionWordRe.test(windowCode);
    const hasTranscodePolicySignal =
      transcodeWordRe.test(windowCode) && transcodePolicyContextRe.test(windowCode);

    if (!hasStrongDecisionSignal && !hasTranscodePolicySignal) continue;

    hits += 1;
    if (hasAllowTag(lines, i)) {
      pushAllowed(allowed, rel, i + 1, 'decision_branching', rawLine);
      continue;
    }

    pushViolation(
      violations,
      rel,
      i + 1,
      1,
      'decision_branching',
      'Branching on decision/policy signals is forbidden in UI. Move logic to backend or mode bridge.',
      rawLine
    );
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
    console.error('❌ Gate L failed: client decision engine patterns detected.\n');
    for (const v of violations) {
      console.error(`- [${v.check}] ${v.file}:${v.line}:${v.col}`);
      console.error(`  reason: ${v.reason}`);
      console.error(`  code:   ${v.snippet}`);
      console.error('  fix:    move policy to backend/mode bridge or mark display-only where purely presentational');
    }
    console.error('\nSummary');
    console.error(`- scanned_files: ${files.length}`);
    console.error(`- hits: ${totalHits}`);
    console.error(`- allowlisted: ${allowed.length}`);
    console.error(`- violations: ${violations.length}`);
    process.exit(1);
  }

  console.log('✅ Gate L passed: no client decision engine patterns.');
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
  console.error('❌ Gate L failed due to execution error.');
  console.error(err instanceof Error ? err.message : String(err));
  process.exit(1);
});
