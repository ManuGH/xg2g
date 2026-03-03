import { describe, expect, it } from 'vitest';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';

describe('V3Player Duration Truth Gate', () => {
  const __dirname = path.dirname(fileURLToPath(import.meta.url));
  const playerPath = path.resolve(__dirname, '../../src/components/V3Player.tsx');
  const resumePath = path.resolve(__dirname, '../../src/features/resume/useResume.ts');

  it('does not use HTMLMediaElement duration as duration truth input', () => {
    const playerSource = fs.readFileSync(playerPath, 'utf-8');
    const resumeSource = fs.readFileSync(resumePath, 'utf-8');

    expect(playerSource).not.toMatch(/\bvideo\.duration\b/);
    expect(resumeSource).not.toMatch(/\bvideoElement\.duration\b/);
  });

  it('consumes durationMs from playback info contract', () => {
    const playerSource = fs.readFileSync(playerPath, 'utf-8');
    expect(playerSource).toMatch(/\bdurationMs\b/);
  });
});
