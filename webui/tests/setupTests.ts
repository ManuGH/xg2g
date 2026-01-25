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

vi.mock('react-i18next', async () => {
  const actual = await vi.importActual<any>('react-i18next');
  return {
    ...actual,
    useTranslation: () => ({
      t: (key: string, opts?: any) => {
        if (typeof opts === 'string') return opts;
        if (opts && typeof opts === 'object' && 'defaultValue' in opts) {
          return String(opts.defaultValue);
        }
        return key;
      },
      i18n: {
        language: 'en',
        changeLanguage: async () => { }
      }
    }),
    Trans: ({ i18nKey, defaults }: { i18nKey?: string; defaults?: string }) =>
      defaults ?? i18nKey
  };
});
