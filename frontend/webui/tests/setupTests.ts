import '@testing-library/jest-dom';
import { vi } from 'vitest';

// Node 26 ships native experimental Web Storage globals (localStorage,
// sessionStorage, Storage). Because vitest's jsdom environment makes
// `window === globalThis`, those natives occupy the storage slots instead of
// jsdom's: `window.localStorage` resolves to Node's native getter, which is
// `undefined` unless the process is started with `--localstorage-file`, so
// every test touching localStorage throws ("Cannot read properties of
// undefined (reading 'clear')"). jsdom's own `Storage` is then unreachable, so
// `new StorageEvent('storage', { storageArea: window.localStorage })` also
// throws ("storageArea is not of type 'Storage'"). Install an in-memory
// Storage plus a permissive StorageEvent when the environment is broken — a
// no-op on Node versions / CI where jsdom provides a working localStorage.
function installWebStorageCompat(): void {
  const works = (() => {
    try {
      return (
        typeof window !== 'undefined' &&
        window.localStorage != null &&
        typeof window.localStorage.clear === 'function'
      );
    } catch {
      return false;
    }
  })();
  if (works) {
    return;
  }

  const makeStorage = (): Storage => {
    const store = new Map<string, string>();
    return {
      get length() {
        return store.size;
      },
      clear() {
        store.clear();
      },
      getItem(key: string) {
        return store.has(key) ? store.get(key)! : null;
      },
      key(index: number) {
        return Array.from(store.keys())[index] ?? null;
      },
      removeItem(key: string) {
        store.delete(key);
      },
      setItem(key: string, value: string) {
        store.set(String(key), String(value));
      },
    } as Storage;
  };

  for (const name of ['localStorage', 'sessionStorage'] as const) {
    const value = makeStorage();
    Object.defineProperty(window, name, { configurable: true, value });
    Object.defineProperty(globalThis, name, { configurable: true, value });
  }

  // jsdom's StorageEvent webidl-validates `storageArea` as a jsdom Storage,
  // which the in-memory shim above is not. Replace it with a permissive event
  // that just carries the init fields; the app only reads `.key`/`.storageArea`.
  const BaseEvent = window.Event;
  class CompatStorageEvent extends BaseEvent {
    key: string | null;
    oldValue: string | null;
    newValue: string | null;
    url: string;
    storageArea: Storage | null;
    constructor(type: string, init: StorageEventInit = {}) {
      super(type, init);
      this.key = init.key ?? null;
      this.oldValue = init.oldValue ?? null;
      this.newValue = init.newValue ?? null;
      this.url = init.url ?? '';
      this.storageArea = init.storageArea ?? null;
    }
  }
  Object.defineProperty(window, 'StorageEvent', { configurable: true, value: CompatStorageEvent });
  Object.defineProperty(globalThis, 'StorageEvent', { configurable: true, value: CompatStorageEvent });
}
installWebStorageCompat();

const mediaProto = globalThis.HTMLMediaElement?.prototype;
if (mediaProto) {
  Object.defineProperty(mediaProto, 'play', {
    configurable: true,
    value: vi.fn().mockResolvedValue(undefined)
  });
  Object.defineProperty(mediaProto, 'pause', {
    configurable: true,
    value: vi.fn()
  });
  Object.defineProperty(mediaProto, 'load', {
    configurable: true,
    value: vi.fn()
  });
}

const videoProto = globalThis.HTMLVideoElement?.prototype;
if (videoProto && !('requestPictureInPicture' in videoProto)) {
  Object.defineProperty(videoProto, 'requestPictureInPicture', {
    configurable: true,
    value: vi.fn().mockResolvedValue(undefined)
  });
}

import enTranslations from '../src/locales/en.json';

const tMock = (key: string, opts?: any) => {
  if (typeof opts === 'string') return opts;

  // Try to find the key in enTranslations
  const keys = key.split('.');
  let val: any = enTranslations;
  for (const k of keys) {
    val = val?.[k];
  }

  if (typeof val === 'string') {
    // Simple interpolation support for {{var}}
    if (opts && typeof opts === 'object') {
      let interpolated = val;
      for (const [k, v] of Object.entries(opts)) {
        interpolated = interpolated.replace(new RegExp(`\\{\\{${k}\\}\\}`, 'g'), String(v));
      }
      return interpolated;
    }
    return val;
  }

  if (opts && typeof opts === 'object') {
    if ('defaultValue' in opts) return String(opts.defaultValue);
    const parts = Object.entries(opts)
      .filter(([k]) => k !== 'defaultValue')
      .map(([k, v]) => `${k}=${v}`);
    return parts.length > 0 ? `${key} (${parts.join(', ')})` : key;
  }
  return key;
};

const i18nMock = {
  language: 'en',
  changeLanguage: async () => { }
};

vi.mock('react-i18next', async () => {
  const actual = await vi.importActual<any>('react-i18next');
  return {
    ...actual,
    useTranslation: () => ({
      t: tMock,
      i18n: i18nMock
    }),
    Trans: ({ i18nKey, defaults }: { i18nKey?: string; defaults?: string }) => {
      if (defaults) return defaults;
      if (!i18nKey) return '';
      const keys = i18nKey.split('.');
      let val: any = enTranslations;
      for (const k of keys) {
        val = val?.[k];
      }
      return typeof val === 'string' ? val : i18nKey;
    }
  };
});
