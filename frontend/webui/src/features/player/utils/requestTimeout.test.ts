import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  HEARTBEAT_REQUEST_TIMEOUT_MS,
  SESSION_REQUEST_TIMEOUT_MS,
  timeoutSignal,
} from './requestTimeout';

describe('timeoutSignal', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('returns a signal that is not yet aborted', () => {
    const signal = timeoutSignal(1000);
    expect(signal).toBeInstanceOf(AbortSignal);
    expect(signal?.aborted).toBe(false);
  });

  it('aborts after the deadline elapses', async () => {
    // AbortSignal.timeout is backed by a platform timer that fake timers do not
    // intercept, so this uses a real (short) deadline and waits for the event.
    const signal = timeoutSignal(20);
    expect(signal?.aborted).toBe(false);
    await new Promise<void>((resolve) => {
      signal?.addEventListener('abort', () => resolve(), { once: true });
    });
    expect(signal?.aborted).toBe(true);
  });

  it('returns a fresh, independent signal on each call', () => {
    const a = timeoutSignal(1000);
    const b = timeoutSignal(1000);
    expect(a).not.toBe(b);
  });

  // Negative control: without AbortSignal.timeout the helper must degrade to
  // undefined (a no-op for fetch) rather than throw — otherwise old engines
  // would lose the request entirely instead of just losing the deadline.
  it('returns undefined when AbortSignal.timeout is unavailable', () => {
    const original = AbortSignal.timeout;
    try {
      // @ts-expect-error intentionally remove the API for the negative control
      AbortSignal.timeout = undefined;
      expect(timeoutSignal(1000)).toBeUndefined();
    } finally {
      AbortSignal.timeout = original;
    }
  });

  it('keeps the heartbeat deadline below the session-control deadline', () => {
    expect(HEARTBEAT_REQUEST_TIMEOUT_MS).toBeLessThan(SESSION_REQUEST_TIMEOUT_MS);
  });
});
