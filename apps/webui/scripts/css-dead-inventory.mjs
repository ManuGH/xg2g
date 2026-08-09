#!/usr/bin/env node

/**
 * css-dead-inventory.mjs
 * 
 * AST-like brace-balanced dead CSS analyzer for CSS Modules.
 * Scans CSS Module files and checks for identifier usages in TS/TSX/JS/JSX files
 * that actually import the given stylesheet.
 * 
 * Features:
 * - Precise word-boundary detection (no substring false-positives)
 * - Ignores :global(...) selectors, comments, and keyframes
 * - Tracks composes: inheritance
 * - Supports allowlist comment: /* dead-css-allow * / or /* dead-css-ignore * /
 * - Scans test files (.test.ts/.test.tsx) to prevent breaking test contracts
 * - Supports scanning a single file or all src/**\/*.module.css files
 * - Returns exit code 1 on violations, 0 on clean state
 */

import { readFileSync, readdirSync, statSync } from 'node:fs';
import path from 'node:path';

const args = process.argv.slice(2);
let targetPattern = args[0] || 'src/**/*.module.css';
let srcDir = args[1] ? path.resolve(process.cwd(), args[1]) : path.resolve(process.cwd(), 'src');

function esc(s) {
  return s.replace(/[-/\\^$*+?.()|[\]{}]/g, '\\$&');
}

function getLineNumber(content, index) {
  let line = 1;
  for (let i = 0; i < index; i++) {
    if (content[i] === '\n') line++;
  }
  return line;
}

function stripCommentsPreserveLines(raw) {
  let clean = '';
  let inComment = false;
  for (let i = 0; i < raw.length; i++) {
    if (!inComment && raw[i] === '/' && raw[i + 1] === '*') {
      inComment = true;
      clean += '  ';
      i++;
    } else if (inComment && raw[i] === '*' && raw[i + 1] === '/') {
      inComment = false;
      clean += '  ';
      i++;
    } else if (inComment) {
      clean += raw[i] === '\n' ? '\n' : ' ';
    } else {
      clean += raw[i];
    }
  }
  return clean;
}

function extractClassesFromSelector(selector) {
  const cls = [];
  const withoutGlobal = selector.replace(/:global\([^)]*\)/g, '');
  const regex = /\.([a-zA-Z_-][a-zA-Z0-9_-]*)/g;
  let match;
  while ((match = regex.exec(withoutGlobal)) !== null) {
    cls.push(match[1]);
  }
  return cls;
}

function parseCssRules(rawCss) {
  const cleanCss = stripCommentsPreserveLines(rawCss);
  const rules = [];
  const declaredClasses = new Set();
  const compositions = new Map();
  const allowlistedClasses = new Set();

  // Check for allowlist comments in rawCss
  const allowRegex = /\/\*\s*dead-css-(?:allow|ignore)\s+([a-zA-Z0-9_,\s-]+)\*\//g;
  let am;
  while ((am = allowRegex.exec(rawCss)) !== null) {
    const list = am[1].split(/[\s,]+/).filter(Boolean);
    list.forEach((c) => allowlistedClasses.add(c));
  }

  function parseBlock(content, startOffset) {
    let pos = 0;
    let depth = 0;
    let selStart = 0;
    let currentSel = '';
    let blockStart = 0;

    while (pos < content.length) {
      const char = content[pos];

      if (char === '{') {
        if (depth === 0) {
          currentSel = content.slice(selStart, pos).trim();
          blockStart = selStart;
        }
        depth++;
      } else if (char === '}') {
        depth--;
        if (depth === 0) {
          const body = content.slice(blockStart, pos + 1);
          const nonWsOffset = content.slice(blockStart).search(/\S/);
          const effectiveBlockStart = blockStart + (nonWsOffset >= 0 ? nonWsOffset : 0);
          const rStartLine = getLineNumber(cleanCss, startOffset + effectiveBlockStart);
          const rEndLine = getLineNumber(cleanCss, startOffset + pos);

          if (currentSel.startsWith('@media') || currentSel.startsWith('@supports')) {
            // Recurse inside media/supports block
            const innerOpen = body.indexOf('{');
            const innerBody = body.slice(innerOpen + 1, -1);
            const innerOffset = startOffset + blockStart + innerOpen + 1;
            parseBlock(innerBody, innerOffset);
          } else if (!currentSel.startsWith('@keyframes')) {
            const ruleClasses = extractClassesFromSelector(currentSel);
            ruleClasses.forEach((c) => declaredClasses.add(c));

            const compMatch = /composes:\s*([^;]+);/.exec(body);
            const compList = [];
            if (compMatch) {
              const comp = compMatch[1].trim().split(/\s+/);
              compList.push(...comp);
              ruleClasses.forEach((c) => {
                if (!compositions.has(c)) compositions.set(c, []);
                compositions.get(c).push(...comp);
              });
            }

            rules.push({
              selector: currentSel,
              startLine: rStartLine,
              endLine: rEndLine,
              classes: ruleClasses,
              composes: compList,
            });
          }

          selStart = pos + 1;
        }
      } else if (depth === 0 && (char === ';' || char === '\n')) {
        if (content.slice(selStart, pos).trim().startsWith('@import')) {
          selStart = pos + 1;
        }
      }

      pos++;
    }
  }

  parseBlock(cleanCss, 0);

  return { rules, declaredClasses, compositions, allowlistedClasses };
}

function walkDir(dir) {
  let results = [];
  const list = readdirSync(dir);
  for (const file of list) {
    const filePath = path.join(dir, file);
    const stat = statSync(filePath);
    if (stat && stat.isDirectory()) {
      if (file !== 'node_modules' && file !== '.git' && file !== 'dist' && file !== '.vite') {
        results = results.concat(walkDir(filePath));
      }
    } else if (/\.(ts|tsx|js|jsx|css)$/.test(file)) {
      results.push(filePath);
    }
  }
  return results;
}

function analyzeCssFile(cssPath, allSourceFiles) {
  const rawCss = readFileSync(cssPath, 'utf8');
  const { rules, declaredClasses, compositions, allowlistedClasses } = parseCssRules(rawCss);

  const cssBasename = path.basename(cssPath);
  const importingFiles = [];

  for (const f of allSourceFiles) {
    if (!/\.(ts|tsx|js|jsx)$/.test(f)) continue;
    const content = readFileSync(f, 'utf8');
    if (content.includes(cssBasename)) {
      const importRegex = new RegExp(
        `import\\s+([a-zA-Z0-9_$]+)\\s+from\\s+['"][^'"]*${esc(cssBasename)}['"]`,
        'g'
      );
      const idents = [];
      let m;
      while ((m = importRegex.exec(content)) !== null) {
        idents.push(m[1]);
      }
      if (idents.length > 0) {
        importingFiles.push({ filePath: f, styleIdentifiers: idents, content });
      }
    }
  }

  const usedClasses = new Set([...allowlistedClasses]);
  const dynamicAccessFiles = [];

  for (const { filePath, styleIdentifiers, content } of importingFiles) {
    for (const ident of styleIdentifiers) {
      for (const cls of declaredClasses) {
        if (usedClasses.has(cls)) continue;

        // Precise boundary check: styles.className or styles['className']
        const hasDotAccess = new RegExp(`\\b${esc(ident)}\\.${esc(cls)}\\b(?![\\w-])`).test(content);
        const hasBracketAccess = new RegExp(`\\b${esc(ident)}\\[\\s*(['"\`])${esc(cls)}\\1\\s*\\]`).test(content);

        if (hasDotAccess || hasBracketAccess) {
          usedClasses.add(cls);
        }
      }

      // Check dynamic indexing: ident[variable]
      const dynamicRegex = new RegExp(`${esc(ident)}\\[([a-zA-Z0-9_$]+)\\]`, 'g');
      let dm;
      while ((dm = dynamicRegex.exec(content)) !== null) {
        if (!dm[1].startsWith("'") && !dm[1].startsWith('"') && !dm[1].startsWith('`')) {
          dynamicAccessFiles.push(`${path.relative(srcDir, filePath)}: ${ident}[${dm[1]}]`);
        }
      }
    }
  }

  // Account for compositions
  for (const [cls, compList] of compositions.entries()) {
    if (usedClasses.has(cls)) {
      compList.forEach((c) => usedClasses.add(c));
    }
  }

  const deadClasses = Array.from(declaredClasses).filter((c) => !usedClasses.has(c)).sort();
  
  // A rule is dead if:
  // For each comma-separated sub-selector: if any class in that sub-selector is dead, that sub-selector is dead.
  // The rule block is dead if ALL its comma-separated sub-selectors are dead.
  function isSubSelectorDead(subSel) {
    const subClasses = extractClassesFromSelector(subSel);
    if (subClasses.length === 0) return false;
    // In a descendant/compound chain (.a.b or .a .b), if ANY class in the chain is dead, the selector never matches.
    return subClasses.some((c) => !usedClasses.has(c));
  }

  const deadRules = rules.filter((r) => {
    if (r.classes.length === 0) return false;
    const subSelectors = r.selector.split(',').map((s) => s.trim()).filter(Boolean);
    return subSelectors.every(isSubSelectorDead);
  });

  let deadLinesCount = 0;
  for (const r of deadRules) {
    deadLinesCount += (r.endLine - r.startLine + 1);
  }

  return {
    cssPath,
    importingFilesCount: importingFiles.length,
    totalClassesCount: declaredClasses.size,
    usedClassesCount: usedClasses.size,
    deadClasses,
    deadRules,
    deadLinesCount,
    dynamicAccessFiles,
  };
}

function main() {
  const allSourceFiles = walkDir(srcDir);
  const targetCssFiles = targetPattern.includes('*')
    ? allSourceFiles.filter((f) => f.endsWith('.module.css'))
    : [path.resolve(process.cwd(), targetPattern)];

  let totalViolations = 0;
  let totalDeadLines = 0;

  for (const cssFile of targetCssFiles) {
    const result = analyzeCssFile(cssFile, allSourceFiles);
    
    console.log(`\n=== CSS Dead Code Inventory ===`);
    console.log(`File: ${path.relative(process.cwd(), result.cssPath)}`);
    console.log(`Imported by: ${result.importingFilesCount} source file(s)`);
    console.log(`Total declared module classes: ${result.totalClassesCount}`);
    console.log(`Active / Used classes: ${result.usedClassesCount}`);
    console.log(`Dead / Unused classes: ${result.deadClasses.length}`);
    console.log(`Dead rules: ${result.deadRules.length} (${result.deadLinesCount} lines)`);

    if (result.dynamicAccessFiles.length > 0) {
      console.log(`\n⚠️ Dynamic styles[key] accesses detected:`);
      result.dynamicAccessFiles.forEach((d) => console.log(`  - ${d}`));
    }

    if (result.deadClasses.length > 0) {
      totalViolations += result.deadClasses.length;
      totalDeadLines += result.deadLinesCount;

      console.log(`\n--- Dead Classes (${result.deadClasses.length}) ---`);
      console.log(result.deadClasses.join(', '));

      console.log(`\n--- Dead Rule Blocks (${result.deadRules.length}) ---`);
      result.deadRules.forEach((r) => {
        console.log(`L${r.startLine}-${r.endLine}: ${r.selector.replace(/\s+/g, ' ').slice(0, 70)}`);
      });
    }
  }

  if (totalViolations > 0) {
    console.error(`\nFAIL: ${totalViolations} dead class(es) across ${totalDeadLines} line(s) detected.`);
    process.exit(1);
  } else {
    console.log(`\nPASS: No dead CSS classes detected.`);
    process.exit(0);
  }
}

main();
