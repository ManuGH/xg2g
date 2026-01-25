
import * as ts from 'typescript';
import * as fs from 'fs';
import * as path from 'path';

// verify-ui-contract-scan.ts
// Scans TypeScript sources for usage of forbidden fields defined in ui_consumption.manifest.json.
// Uses TypeScript Compiler API for AST traversal.

const MANIFEST_PATH = path.join(__dirname, '../contracts/ui_consumption.manifest.json');
const SRC_DIR = path.join(__dirname, '../webui/src');

// Load prohibited fields
const manifest = JSON.parse(fs.readFileSync(MANIFEST_PATH, 'utf-8'));
const forbiddenEntries = manifest.entries.filter((e: any) => e.category === 'forbidden');

// Map of forbidden field names to their full paths for error messaging
// e.g. "outputs" -> "decision.outputs"
const forbiddenFields = new Map<string, string>();
forbiddenEntries.forEach((e: any) => {
  const parts = e.fieldPath.split('.');
  const fieldName = parts[parts.length - 1]; // "outputs" from "decision.outputs"
  forbiddenFields.set(fieldName, e.fieldPath);
});

console.log('--- AST Contract Scan ---');
console.log(`Forbidden Fields: ${Array.from(forbiddenFields.keys()).join(', ')}`);

let errorCount = 0;

function checkNode(node: ts.Node, sourceFile: ts.SourceFile) {
  // 1. Property Access: obj.forbidden
  if (ts.isPropertyAccessExpression(node)) {
    const name = node.name.text;
    if (forbiddenFields.has(name)) {
      reportError(node, sourceFile, name, 'Property Access');
    }
  }

  // 2. Element Access: obj["forbidden"]
  if (ts.isElementAccessExpression(node)) {
    if (ts.isStringLiteral(node.argumentExpression)) {
      const name = node.argumentExpression.text;
      if (forbiddenFields.has(name)) {
        reportError(node, sourceFile, name, 'Element Access');
      }
    }
  }

  // 3. Destructuring: const { forbidden } = obj
  // Also covers function params: function({ forbidden }) {}
  if (ts.isBindingElement(node)) {
    // If it has a propertyName (renaming: { forbidden: f }), check propertyName.
    // If not, check name (shorthand: { forbidden }).
    let name = '';
    if (node.propertyName && ts.isIdentifier(node.propertyName)) {
      name = node.propertyName.text;
    } else if (ts.isIdentifier(node.name)) {
      name = node.name.text;
    }

    if (name && forbiddenFields.has(name)) {
      reportError(node, sourceFile, name, 'Destructuring');
    }
  }

  // 4. Object Literal Property Assignment (Key): { forbidden: val }
  // Often used when constructing the forbidden object to send to backend.
  if (ts.isPropertyAssignment(node)) {
    let name = '';
    if (ts.isIdentifier(node.name)) {
      name = node.name.text;
    } else if (ts.isStringLiteral(node.name)) {
      name = node.name.text;
    }

    if (name && forbiddenFields.has(name)) {
      // Context matters: assignments might be allowed if we ARE the mock/test?
      // We'll catch it and can ignore if it's a test file (handled in file walker).
      reportError(node, sourceFile, name, 'Property Assignment');
    }
  }

  ts.forEachChild(node, (child) => checkNode(child, sourceFile));
}

function reportError(node: ts.Node, sourceFile: ts.SourceFile, fieldName: string, type: string) {
  const { line, character } = sourceFile.getLineAndCharacterOfPosition(node.getStart());
  const contractPath = forbiddenFields.get(fieldName);
  console.error(
    `❌ Forbidden Consumption [${type}]\n` +
    `   File: ${sourceFile.fileName}:${line + 1}:${character + 1}\n` +
    `   Field: '${fieldName}' (Contract: ${contractPath})\n` +
    `   Rationale: This field is marked forbidden in ui_consumption.manifest.json.\n`
  );
  errorCount++;
}

function walkDir(dir: string) {
  const files = fs.readdirSync(dir);
  for (const file of files) {
    const fullPath = path.join(dir, file);
    const stat = fs.statSync(fullPath);

    if (stat.isDirectory()) {
      if (file !== 'client-ts' && file !== 'client-legacy') { // Exclude generated clients
        walkDir(fullPath);
      }
    } else if (fullPath.endsWith('.ts') || fullPath.endsWith('.tsx')) {
      if (file.endsWith('.test.ts') || file.endsWith('.test.tsx')) continue; // Skip tests? No, maybe strict scan there too except mocks?
      // For now, scan everything except generated clients.

      // EXCEPTION: Skip this script itself if it scans itself?? (It's in scripts/, scan is src/)
      scanFile(fullPath);
    }
  }
}

function scanFile(filePath: string) {
  const code = fs.readFileSync(filePath, 'utf-8');
  const sourceFile = ts.createSourceFile(
    filePath,
    code,
    ts.ScriptTarget.Latest,
    true
  );
  checkNode(sourceFile, sourceFile);
}

// Start
try {
  walkDir(SRC_DIR);
} catch (e) {
  console.error("Scan failed:", e);
  process.exit(1);
}

if (errorCount > 0) {
  console.error(`\nFAILED: Found ${errorCount} forbidden contract violations.`);
  process.exit(1);
}

console.log(`\nPASSED: No forbidden contract violations found.`);
process.exit(0);
