import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useDelayedFlag } from './useDelayedFlag';

describe('useDelayedFlag (spinner-card debounce)', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  // Negative control: with delayMs=0 (no debounce) this goes true immediately and
  // the 400ms assertion flips red — proving the delay is what suppresses the flash.
  it('stays false before the delay elapses (no card flash for fast work)', () => {
    const { result } = renderHook(() => useDelayedFlag(true, 500));
    expect(result.current).toBe(false);
    act(() => vi.advanceTimersByTime(400));
    expect(result.current).toBe(false);
  });

  it('shows the card once the delay elapses while still preparing (a real wait)', () => {
    const { result } = renderHook(() => useDelayedFlag(true, 500));
    act(() => vi.advanceTimersByTime(500));
    expect(result.current).toBe(true);
  });

  // The fast-resume case: the re-prepare finishes before the delay, so the card never
  // flashes — while the cover (driven separately) stayed up the whole time.
  it('never shows the card if preparing clears before the delay', () => {
    const { result, rerender } = renderHook(({ active }) => useDelayedFlag(active, 500), {
      initialProps: { active: true },
    });
    act(() => vi.advanceTimersByTime(300));
    rerender({ active: false });
    act(() => vi.advanceTimersByTime(500));
    expect(result.current).toBe(false);
  });
});
