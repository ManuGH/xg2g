
import * as fs from 'fs';
import * as path from 'path';
import Ajv from 'ajv';

// verify-telemetry-schema.ts
// Validates the Telemetry JSON Schema itself is valid.

const REPO_ROOT = path.resolve(__dirname, '..');
const SCHEMA_FILE = path.join(REPO_ROOT, 'contracts/telemetry.schema.json');

const ajv = new Ajv({ strict: true });

try {
  const schemaContent = fs.readFileSync(SCHEMA_FILE, 'utf-8');
  const schema = JSON.parse(schemaContent);

  // Compile schema to verify validity
  const validate = ajv.compile(schema);

  console.log("✅ Telemetry Schema is valid JSON Schema Draft-07/2019-09.");
} catch (e) {
  console.error("❌ Telemetry Schema is INVALID.");
  console.error(e);
  process.exit(1);
}
