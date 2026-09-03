import { useEffect, useRef } from 'react';

export interface EngineTimerRegistry {
  /**
   * Schedules a named timeout. If a timeout with the same name already exists,
   * it is cancelled before the new one is scheduled.
   */
  setTimeout(name: string, fn: () => void, delayMs: number): number;

  /**
   * Cancels a named timeout if currently pending.
   */
  clearTimeout(name: string): void;

  /**
   * Returns true if a timeout with the given name is currently active.
   */
  hasTimeout(name: string): boolean;

  /**
   * Schedules a named interval. If an interval with the same name already exists,
   * it is cancelled before the new one is scheduled.
   */
  setInterval(name: string, fn: () => void, intervalMs: number): number;

  /**
   * Cancels a named interval if currently pending.
   */
  clearInterval(name: string): void;

  /**
   * Returns true if an interval with the given name is currently active.
   */
  hasInterval(name: string): boolean;

  /**
   * Atomically clears all pending timeouts and intervals managed by this registry.
   */
  clearAll(): void;

  /**
   * Cancellable delay honoring an optional AbortSignal.
   * Cleans up its internal timer if aborted.
   */
  delay(delayMs: number, signal?: AbortSignal): Promise<void>;
}

export function createEngineTimerRegistry(): EngineTimerRegistry {
  const timeouts = new Map<string, number>();
  const intervals = new Map<string, number>();

  const clearTimeout = (name: string): void => {
    const existing = timeouts.get(name);
    if (existing !== undefined) {
      window.clearTimeout(existing);
      timeouts.delete(name);
    }
  };

  const setTimeout = (name: string, fn: () => void, delayMs: number): number => {
    clearTimeout(name);
    const id = window.setTimeout(() => {
      timeouts.delete(name);
      fn();
    }, delayMs);
    timeouts.set(name, id);
    return id;
  };

  const hasTimeout = (name: string): boolean => timeouts.has(name);

  const clearInterval = (name: string): void => {
    const existing = intervals.get(name);
    if (existing !== undefined) {
      window.clearInterval(existing);
      intervals.delete(name);
    }
  };

  const setInterval = (name: string, fn: () => void, intervalMs: number): number => {
    clearInterval(name);
    const id = window.setInterval(fn, intervalMs);
    intervals.set(name, id);
    return id;
  };

  const hasInterval = (name: string): boolean => intervals.has(name);

  const clearAll = (): void => {
    for (const id of timeouts.values()) {
      window.clearTimeout(id);
    }
    timeouts.clear();

    for (const id of intervals.values()) {
      window.clearInterval(id);
    }
    intervals.clear();
  };

  const delay = (delayMs: number, signal?: AbortSignal): Promise<void> => {
    if (signal?.aborted) {
      return Promise.reject(new DOMException('Aborted', 'AbortError'));
    }
    return new Promise<void>((resolve, reject) => {
      let timerId: number | undefined;
      const onAbort = () => {
        if (timerId !== undefined) {
          window.clearTimeout(timerId);
        }
        reject(new DOMException('Aborted', 'AbortError'));
      };

      if (signal) {
        signal.addEventListener('abort', onAbort, { once: true });
      }

      timerId = window.setTimeout(() => {
        if (signal) {
          signal.removeEventListener('abort', onAbort);
        }
        resolve();
      }, delayMs);
    });
  };

  return {
    setTimeout,
    clearTimeout,
    hasTimeout,
    setInterval,
    clearInterval,
    hasInterval,
    clearAll,
    delay,
  };
}

export function useEngineTimerRegistry(): EngineTimerRegistry {
  const registryRef = useRef<EngineTimerRegistry | null>(null);
  if (!registryRef.current) {
    registryRef.current = createEngineTimerRegistry();
  }

  useEffect(() => {
    const registry = registryRef.current;
    return () => {
      registry?.clearAll();
    };
  }, []);

  return registryRef.current;
}
