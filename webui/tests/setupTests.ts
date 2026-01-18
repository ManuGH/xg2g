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
