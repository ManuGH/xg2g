import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  readLocalStorageItem,
  removeLocalStorageItem,
  safeLocalStorage,
  writeLocalStorageItem,
} from './safeStorage';

function defineLocalStorage(value: unknown): void {
  Object.defineProperty(window, 'localStorage', {
    configurable: true,
    get: typeof value === 'function' ? (value as () => Storage) : () => value as Storage,
  });
}

const originalDescriptor = Object.getOwnPropertyDescriptor(window, 'localStorage');

afterEach(() => {
  if (originalDescriptor) {
    Object.defineProperty(window, 'localStorage', originalDescriptor);
  }
  vi.restoreAllMocks();
});

describe('safeStorage', () => {
  it('reads, writes and removes when storage works', () => {
    const backing = new Map<string, string>();
    defineLocalStorage({
      getItem: (k: string) => backing.get(k) ?? null,
      setItem: (k: string, v: string) => void backing.set(k, v),
      removeItem: (k: string) => void backing.delete(k),
    });

    writeLocalStorageItem('k', 'v');
    expect(readLocalStorageItem('k')).toBe('v');
    removeLocalStorageItem('k');
    expect(readLocalStorageItem('k')).toBeNull();
  });

  it('never throws when the localStorage getter throws (blocked cookies)', () => {
    defineLocalStorage(() => {
      throw new DOMException('blocked', 'SecurityError');
    });

    expect(safeLocalStorage()).toBeNull();
    expect(readLocalStorageItem('k')).toBeNull();
    expect(() => writeLocalStorageItem('k', 'v')).not.toThrow();
    expect(() => removeLocalStorageItem('k')).not.toThrow();
  });

  it('never throws when an operation throws (quota / disabled)', () => {
    defineLocalStorage({
      getItem: () => {
        throw new Error('getItem blocked');
      },
      setItem: () => {
        throw new DOMException('quota', 'QuotaExceededError');
      },
      removeItem: () => {
        throw new Error('removeItem blocked');
      },
    });

    expect(readLocalStorageItem('k')).toBeNull();
    expect(() => writeLocalStorageItem('k', 'v')).not.toThrow();
    expect(() => removeLocalStorageItem('k')).not.toThrow();
  });
});
