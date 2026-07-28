import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { importWithRetry } from './lazyWithRetry';

describe('importWithRetry', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    try {
      window.sessionStorage.clear();
    } catch {
      // ignore
    }
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('resolves on the first successful import', async () => {
    const mod = { default: 'C' };
    const result = await importWithRetry(() => Promise.resolve(mod));
    expect(result).toBe(mod);
  });

  it('retries a transient rejection and then resolves', async () => {
    const mod = { default: 'C' };
    let calls = 0;
    const factory = () => {
      calls += 1;
      return calls === 1 ? Promise.reject(new Error('flaky')) : Promise.resolve(mod);
    };
    const result = await importWithRetry(factory, { retries: 2 });
    expect(result).toBe(mod);
    expect(calls).toBe(2);
  });

  it('reloads exactly once when import keeps failing (guarded against loops)', async () => {
    const reload = vi.fn();
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...window.location, reload },
    });

    // First exhausted run -> sets flag and reloads (does not reject).
    const p1 = importWithRetry(() => Promise.reject(new Error('gone')), { retries: 0 });
    await Promise.resolve();
    await Promise.resolve();
    expect(reload).toHaveBeenCalledTimes(1);
    expect(window.sessionStorage.getItem('xg2g_chunk_reload')).toBe('1');
    void p1;

    // Second time the flag is already set -> no further reload, rejects instead.
    reload.mockClear();
    await expect(importWithRetry(() => Promise.reject(new Error('gone')), { retries: 0 })).rejects.toThrow('gone');
    expect(reload).not.toHaveBeenCalled();
  });
});
