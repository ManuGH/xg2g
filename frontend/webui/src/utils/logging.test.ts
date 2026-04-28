import { afterEach, describe, expect, it, vi } from 'vitest';

import { debugError, debugLog, debugWarn } from './logging';

describe('debug logging', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('stays quiet during tests unless explicitly enabled', () => {
    const log = vi.spyOn(console, 'log').mockImplementation(() => {});
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const error = vi.spyOn(console, 'error').mockImplementation(() => {});

    debugLog('hidden log');
    debugWarn('hidden warn');
    debugError('hidden error');

    expect(log).not.toHaveBeenCalled();
    expect(warn).not.toHaveBeenCalled();
    expect(error).not.toHaveBeenCalled();
  });
});
