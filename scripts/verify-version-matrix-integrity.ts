
import * as fs from 'fs';
import * as path from 'path';
import Ajv from 'ajv';

// verify-version-matrix-integrity.ts
// Validates the Version Matrix artifact against its schema.

const REPO_ROOT = path.resolve(__dirname, '..');
const SCHEMA_FILE = path.join(REPO_ROOT, 'contracts/version_matrix.schema.json');
const DATA_FILE = path.join(REPO_ROOT, 'contracts/version_matrix.json');

const ajv = new Ajv({ strict: true });

try {
  const schemaContent = fs.readFileSync(SCHEMA_FILE, 'utf-8');
  const dataContent = fs.readFileSync(DATA_FILE, 'utf-8');

  const schema = JSON.parse(schemaContent);
  const data = JSON.parse(dataContent);

  const validate = ajv.compile(schema);
  const valid = validate(data);

  if (!valid) {
    console.error("❌ Version Matrix is INVALID.");
    console.error(validate.errors);
    process.exit(1);
  }

  console.log("✅ Version Matrix integrity verified.");
} catch (e) {
  console.error("❌ Verification failed:", e);
  process.exit(1);
}
