/// <reference types="@testing-library/jest-dom" />
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import V3Player from './V3Player';
import Hls from '../lib/hlsRuntime';
import type { V3PlayerProps } from '../types/v3-player';

vi.mock('../lib/hlsRuntime', () => {
  const HlsMock = vi.fn().mockImplementation(function (this: any) {
    return {
      on: vi.fn(),
      loadSource: vi.fn(),
      attachMedia: vi.fn(),
      destroy: vi.fn(),
      recoverMediaError: vi.fn(),
    };
  });

  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  (HlsMock as any).Events = {
    LEVEL_SWITCHED: 'hlsLevelSwitched',
    MANIFEST_PARSED: 'hlsManifestParsed',
    FRAG_LOADED: 'hlsFragLoaded',
    ERROR: 'hlsError'
  };
  (HlsMock as any).ErrorTypes = { NETWORK_ERROR: 'networkError' };
  (HlsMock as any).ErrorDetails = { MANIFEST_LOAD_ERROR: 'manifestLoadError' };

  return { default: HlsMock };
});

vi.mock('../client-ts', () => ({
  createSession: vi.fn(),
  postRecordingPlaybackInfo: vi.fn(),
  postLivePlaybackInfo: vi.fn()
}));

describe('V3Player Mobile Controls', () => {
  let maxTouchPointsDescriptor: PropertyDescriptor | undefined;
  let webkitEnterFullscreenDescriptor: PropertyDescriptor | undefined;
  let pictureInPictureEnabledDescriptor: PropertyDescriptor | undefined;
  let requestPictureInPictureDescriptor: PropertyDescriptor | undefined;
  let requestFullscreenDescriptor: PropertyDescriptor | undefined;
  const webkitEnterFullscreen = vi.fn();
  const requestFullscreen = vi.fn().mockResolvedValue(undefined);

  beforeEach(() => {
    vi.clearAllMocks();
    maxTouchPointsDescriptor = Object.getOwnPropertyDescriptor(window.navigator, 'maxTouchPoints');
    webkitEnterFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitEnterFullscreen');
    pictureInPictureEnabledDescriptor = Object.getOwnPropertyDescriptor(document, 'pictureInPictureEnabled');
    requestPictureInPictureDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'requestPictureInPicture');
    requestFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLDivElement.prototype, 'requestFullscreen');

    Object.defineProperty(window.navigator, 'maxTouchPoints', {
      configurable: true,
      value: 5
    });
    Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
      configurable: true,
      value: webkitEnterFullscreen
    });
    Object.defineProperty(document, 'pictureInPictureEnabled', {
      configurable: true,
      value: false
    });
    Object.defineProperty(HTMLDivElement.prototype, 'requestFullscreen', {
      configurable: true,
      value: requestFullscreen
    });

    if (requestPictureInPictureDescriptor) {
      delete (HTMLVideoElement.prototype as any).requestPictureInPicture;
    }

    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation(function (this: HTMLMediaElement, type: string) {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      return '';
    });
  });

  afterEach(() => {
    if (webkitEnterFullscreenDescriptor) {
      Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', webkitEnterFullscreenDescriptor);
    } else {
      delete (HTMLVideoElement.prototype as any).webkitEnterFullscreen;
    }

    if (requestPictureInPictureDescriptor) {
      Object.defineProperty(HTMLVideoElement.prototype, 'requestPictureInPicture', requestPictureInPictureDescriptor);
    } else {
      delete (HTMLVideoElement.prototype as any).requestPictureInPicture;
    }
    if (requestFullscreenDescriptor) {
      Object.defineProperty(HTMLDivElement.prototype, 'requestFullscreen', requestFullscreenDescriptor);
    } else {
      delete (HTMLDivElement.prototype as any).requestFullscreen;
    }

    if (pictureInPictureEnabledDescriptor) {
      Object.defineProperty(document, 'pictureInPictureEnabled', pictureInPictureEnabledDescriptor);
    } else {
      delete (document as any).pictureInPictureEnabled;
    }

    if (maxTouchPointsDescriptor) {
      Object.defineProperty(window.navigator, 'maxTouchPoints', maxTouchPointsDescriptor);
    }

    vi.restoreAllMocks();
  });

  it('keeps wrapper fullscreen on touch devices when native HLS is preferred', async () => {
    const props = {
      src: 'http://example.com/playlist.m3u8',
      autoStart: true
    } as V3PlayerProps;
    const { container } = render(<V3Player {...props} />);

    await waitFor(() => {
      expect(Hls).not.toHaveBeenCalled();
    });

    const fullscreenButton = await screen.findByRole('button', { name: /fullscreen/i });
    fireEvent.click(fullscreenButton);

    const video = container.querySelector('video') as HTMLVideoElement;
    expect(requestFullscreen).toHaveBeenCalledTimes(1);
    expect(webkitEnterFullscreen).not.toHaveBeenCalled();
    expect(video.controls).toBe(false);
    expect(screen.queryByRole('button', { name: /player\.dvrMode/i })).not.toBeInTheDocument();
  });

  it('hides volume controls on mobile WebKit when the native HLS path is active', async () => {
    const props = {
      src: 'http://example.com/playlist.m3u8',
      autoStart: true
    } as V3PlayerProps;
    render(<V3Player {...props} />);

    await waitFor(() => {
      expect(Hls).not.toHaveBeenCalled();
    });

    expect(screen.queryByRole('slider')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /player\.pipLabel/i })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /fullscreen/i })).toBeInTheDocument();
  });
});
