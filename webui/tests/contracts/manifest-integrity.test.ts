import { describe, it, expect } from 'vitest';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';

describe('UI Consumption Manifest Integrity', () => {
  const __dirname = path.dirname(fileURLToPath(import.meta.url));
  const manifestPath = path.resolve(__dirname, '../../../contracts/ui_consumption.manifest.json');
  const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf-8'));

  it('contains no duplicate endpoint+fieldPath pairs', () => {
    const keys = manifest.entries.map((e: any) => `${e.endpoint}:${e.fieldPath}`);
    const uniqueKeys = new Set(keys);
    expect(keys.length).toBe(uniqueKeys.size);
  });

  it('all normative/legacy entries have a rationale', () => {
    manifest.entries.forEach((e: any) => {
      if (['normative', 'legacy'].includes(e.category)) {
        expect(e.rationale.length).toBeGreaterThan(10);
      }
    });
  });

  it('is sorted deterministically by endpoint then fieldPath', () => {
    const keys = manifest.entries.map((e: any) => `${e.endpoint}:${e.fieldPath}`);
    const sortedKeys = [...keys].sort();
    expect(keys).toEqual(sortedKeys);
  });

  it('all entries follow schema categories', () => {
    const validCategories = ['normative', 'legacy', 'forbidden', 'telemetry'];
    manifest.entries.forEach((e: any) => {
      expect(validCategories).toContain(e.category);
    });
  });
});
