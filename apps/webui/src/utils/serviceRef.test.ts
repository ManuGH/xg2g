// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { describe, expect, it } from 'vitest';
import { canonicalServiceRef, matchServiceRef } from './serviceRef';

describe('serviceRef utilities', () => {
  const openWebifRefWithColon: string = '1:0:19:AAAA:BBB:1:C00000:0:0:0:';
  const backendTrimmedRef: string = '1:0:19:AAAA:BBB:1:C00000:0:0:0';

  it('canonicalizes trailing colons and normalizes hex case', () => {
    expect(canonicalServiceRef(openWebifRefWithColon)).toBe(backendTrimmedRef);
    expect(canonicalServiceRef('  1:0:1:1:1:1:0:0:0:0:  ')).toBe('1:0:1:1:1:1:0:0:0:0');
    expect(canonicalServiceRef('')).toBe('');
    expect(canonicalServiceRef(null)).toBe('');
  });

  it('matches service references across trailing colon and casing differences', () => {
    expect(matchServiceRef(openWebifRefWithColon, backendTrimmedRef)).toBe(true);
    expect(matchServiceRef('1:0:1:1:1:1:0:0:0:0', '1:0:1:2:1:1:0:0:0:0')).toBe(false);
    expect(matchServiceRef(null, backendTrimmedRef)).toBe(false);
  });

});
