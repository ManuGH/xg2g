import '@testing-library/jest-dom';
import { vi } from 'vitest';

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
