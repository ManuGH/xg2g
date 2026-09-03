import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { createEngineTimerRegistry, useEngineTimerRegistry } from './engineTimerRegistry';

describe('engineTimerRegistry', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('schedules and executes a named timeout', () => {
    const registry = createEngineTimerRegistry();
    const fn = vi.fn();

    registry.setTimeout('test', fn, 100);
    expect(registry.hasTimeout('test')).toBe(true);
    expect(fn).not.toHaveBeenCalled();

    vi.advanceTimersByTime(100);
    expect(fn).toHaveBeenCalledTimes(1);
    expect(registry.hasTimeout('test')).toBe(false);
  });

  it('cancels existing timeout when same name is scheduled again', () => {
    const registry = createEngineTimerRegistry();
    const fn1 = vi.fn();
    const fn2 = vi.fn();

    registry.setTimeout('test', fn1, 100);
    vi.advanceTimersByTime(50);
    registry.setTimeout('test', fn2, 100);

    vi.advanceTimersByTime(50);
    expect(fn1).not.toHaveBeenCalled();

    vi.advanceTimersByTime(50);
    expect(fn2).toHaveBeenCalledTimes(1);
  });

  it('clears specific named timeout', () => {
    const registry = createEngineTimerRegistry();
    const fn = vi.fn();

    registry.setTimeout('test', fn, 100);
    registry.clearTimeout('test');
    expect(registry.hasTimeout('test')).toBe(false);

    vi.advanceTimersByTime(200);
    expect(fn).not.toHaveBeenCalled();
  });

  it('atomically clears all timers on clearAll', () => {
    const registry = createEngineTimerRegistry();
    const fn1 = vi.fn();
    const fn2 = vi.fn();

    registry.setTimeout('t1', fn1, 100);
    registry.setTimeout('t2', fn2, 200);

    registry.clearAll();
    expect(registry.hasTimeout('t1')).toBe(false);
    expect(registry.hasTimeout('t2')).toBe(false);

    vi.runAllTimers();
    expect(fn1).not.toHaveBeenCalled();
    expect(fn2).not.toHaveBeenCalled();
  });

  it('resolves delay when timeout elapses', async () => {
    const registry = createEngineTimerRegistry();
    const promise = registry.delay(500);

    vi.advanceTimersByTime(500);
    await expect(promise).resolves.toBeUndefined();
  });

  it('rejects delay immediately when AbortSignal is already aborted', async () => {
    const registry = createEngineTimerRegistry();
    const controller = new AbortController();
    controller.abort();

    await expect(registry.delay(500, controller.signal)).rejects.toThrow(/Aborted/);
  });

  it('cancels delay when AbortSignal aborts mid-flight', async () => {
    const registry = createEngineTimerRegistry();
    const controller = new AbortController();
    const promise = registry.delay(500, controller.signal);

    vi.advanceTimersByTime(200);
    controller.abort();

    await expect(promise).rejects.toThrow(/Aborted/);
  });

  it('cancels pending delay when clearAll() is called', async () => {
    const registry = createEngineTimerRegistry();
    const promise = registry.delay(500);

    vi.advanceTimersByTime(200);
    registry.clearAll();

    await expect(promise).rejects.toThrow(/Aborted/);
  });

  it('useEngineTimerRegistry hook clears all timers and delays on unmount', async () => {
    const { result, unmount } = renderHook(() => useEngineTimerRegistry());
    const fn = vi.fn();

    result.current.setTimeout('unmount_test', fn, 500);
    expect(result.current.hasTimeout('unmount_test')).toBe(true);
    const delayPromise = result.current.delay(500);

    unmount();
    vi.runAllTimers();
    expect(fn).not.toHaveBeenCalled();
    await expect(delayPromise).rejects.toThrow(/Aborted/);
  });
});

describe('engineTimerRegistry intervals', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('schedules and executes a named interval', () => {
    const registry = createEngineTimerRegistry();
    const fn = vi.fn();

    registry.setInterval('tick', fn, 100);
    expect(registry.hasInterval('tick')).toBe(true);
    expect(fn).not.toHaveBeenCalled();

    vi.advanceTimersByTime(250);
    expect(fn).toHaveBeenCalledTimes(2);

    registry.clearInterval('tick');
    expect(registry.hasInterval('tick')).toBe(false);

    vi.advanceTimersByTime(200);
    expect(fn).toHaveBeenCalledTimes(2);
  });

  it('clears intervals on clearAll', () => {
    const registry = createEngineTimerRegistry();
    const fn = vi.fn();

    registry.setInterval('tick', fn, 100);
    registry.clearAll();
    expect(registry.hasInterval('tick')).toBe(false);

    vi.advanceTimersByTime(500);
    expect(fn).not.toHaveBeenCalled();
  });
});
