
import * as fs from 'fs';
import * as path from 'path';
import { execSync } from 'child_process';

const TARGET_FILE = path.join(__dirname, '../webui/src/contracts/PolicyEngine.ts');
const TEST_CMD = 'npx vitest run tests/contracts/policy.test.ts';

function runTests() {
  try {
    execSync(`cd webui && ${TEST_CMD}`, { stdio: 'ignore' });
    return true;
  } catch {
    return false;
  }
}

const originalContent = fs.readFileSync(TARGET_FILE, 'utf-8');

const MUTATIONS = [
  {
    name: "Allow Fallback when forbidden",
    from: /canFallback = false;/,
    to: "canFallback = true;"
  },
  {
    name: "Disable FailClosed Check",
    // Target the one in resolvePlaybackInfoPolicy which has the comment "(Hard stops)"
    from: /\/\/ 1\. Fail Closed checks \(Hard stops\)\s+if \(policy.failClosedIf\) \{/,
    to: "// 1. Fail Closed checks (Hard stops)\n if (false) {"
  }
];

let killed = 0;

console.log(`--- Verifying Policy Engine Mutation Kill Rate ---`);

for (const m of MUTATIONS) {
  console.log(`[MUTATION] ${m.name}`);
  const mutated = originalContent.replace(m.from, m.to);
  if (mutated === originalContent) {
    console.error("❌ Mutation failed to apply (Regex mismatch)");
    continue;
  }

  fs.writeFileSync(TARGET_FILE, mutated);
  const passed = runTests();

  if (!passed) {
    console.log("✅ KILLED");
    killed++;
  } else {
    console.error("❌ SURVIVED (Test suite failed to detect mutation)");
  }
}

// Restore
fs.writeFileSync(TARGET_FILE, originalContent);

if (killed === MUTATIONS.length) {
  console.log(`\n✅ ALL MUTATIONS KILLED (${killed}/${MUTATIONS.length})`);
  process.exit(0);
} else {
  console.error(`\n❌ SOME MUTATIONS SURVIVED (${killed}/${MUTATIONS.length})`);
  process.exit(1);
}
