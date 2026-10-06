// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { describe, expect, it } from 'vitest';
import { formatRecordingDescription } from './recordingDescription';

describe('formatRecordingDescription', () => {
  const markedDescription = '[xg2g-rule:123e4567-e89b-12d3-a456-426614174000] Example Show episode 100: Sample description';
  const cleanDescription = 'Example Show episode 100: Sample description';

  it('strips leading [xg2g-rule:<uuid>] marker and following whitespace', () => {
    expect(formatRecordingDescription(markedDescription)).toBe(cleanDescription);
    expect(formatRecordingDescription('[xg2g-rule:abc-123]\nZweite Zeile')).toBe('Zweite Zeile');
    expect(formatRecordingDescription('[XG2G-RULE:XYZ-999] Großgeschrieben')).toBe('Großgeschrieben');
  });

  it('leaves descriptions without markers intact', () => {
    expect(formatRecordingDescription(cleanDescription)).toBe(cleanDescription);
    expect(formatRecordingDescription('Normale Sendungsbeschreibung')).toBe('Normale Sendungsbeschreibung');
    expect(formatRecordingDescription('')).toBe('');
    expect(formatRecordingDescription(null)).toBe('');
    expect(formatRecordingDescription(undefined)).toBe('');
  });

});
