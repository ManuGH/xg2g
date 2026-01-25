
import * as fs from 'fs';
import * as path from 'path';
import { execSync } from 'child_process';

// verify-mutation-kill.ts
// Applies specific mutations to source code and ensures tests FAIL.

const REPO_ROOT = path.resolve(__dirname, '..');
const TARGET_FILE = path.join(REPO_ROOT, 'webui/src/components/V3Player.tsx');
const TEST_FAILCLOSED = 'webui/tests/contracts/v3player.failclosed.test.tsx';
const TEST_ERRORMAP = 'webui/tests/contracts/error-map.matrix.test.tsx';

// Colors
const RED = '\x1b[31m';
const GREEN = '\x1b[32m';
const RESET = '\x1b[0m';

function runTest(testFile: string): boolean {
  try {
    // Run specific test file, silence output
    execSync(`npx vitest run ${testFile}`, {
      cwd: path.join(REPO_ROOT, 'webui'),
      stdio: 'ignore'
    });
    return true; // Passed
  } catch (e) {
    return false; // Failed
  }
}

function applyMutation(content: string, mutationName: string): string {
  if (mutationName === 'RemoveDecisionGuard') {
    // Remove 'else if (pInfo.decision)' block
    // We look for the exact string or close signature
    return content.replace(
      '} else if (pInfo.decision) {',
      '} else if (false) {'
    );
  }
  if (mutationName === 'Remove409Handling') {
    // Disable 409 check
    return content.replace(
      'if (response.status === 409) {',
      'if (false && response.status === 409) {'
    );
  }
  return content;
}

async function verifyMutation(name: string, testFile: string) {
  console.log(`\n🧪 Testing Mutation: ${name}`);

  // 1. Backup
  const originalContent = fs.readFileSync(TARGET_FILE, 'utf-8');

  try {
    // 2. Apply
    const mutatedContent = applyMutation(originalContent, name);
    if (mutatedContent === originalContent) {
      console.error(`${RED}❌ Mutation failed to apply (code pattern not found).${RESET}`);
      return false;
    }
    fs.writeFileSync(TARGET_FILE, mutatedContent);

    // 3. Run Test
    console.log(`   Running test: ${testFile}...`);
    const passed = runTest(testFile);

    // 4. Evaluate
    if (!passed) {
      console.log(`${GREEN}✅ Mutation KILLED (Test incorrectly failed as expected).${RESET}`);
      return true;
    } else {
      console.log(`${RED}❌ Mutation SURVIVED (Test passed despite broken code).${RESET}`);
      return false;
    }

  } finally {
    // 5. Restore
    fs.writeFileSync(TARGET_FILE, originalContent);
  }
}

async function run() {
  console.log("--- Mutation verification ---");
  let allKilled = true;

  // Mutation 1: Remove "Fail Closed" guard
  // Expect: v3player.failclosed.test.tsx to FAIL (because it should fallback to legacy)
  if (!await verifyMutation('RemoveDecisionGuard', TEST_FAILCLOSED)) allKilled = false;

  // Mutation 2: Remove 409 handling
  // Expect: error-map.matrix.test.tsx to FAIL (because it won't show retry hint)
  if (!await verifyMutation('Remove409Handling', TEST_ERRORMAP)) allKilled = false;

  if (allKilled) {
    console.log(`\n${GREEN}💯 All mutations killed. Contract integrity verified.${RESET}`);
    process.exit(0);
  } else {
    console.log(`\n${RED}💀 Some mutations matched. Verification failed.${RESET}`);
    process.exit(1);
  }
}

run();
