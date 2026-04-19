/// <reference types="@testing-library/jest-dom" />
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import V3Player from './V3Player';
import Hls from '../lib/hlsRuntime';
import type { V3PlayerProps } from '../../../types/v3-player';
import styles from './V3Player.module.css';

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
  let visibilityStateDescriptor: PropertyDescriptor | undefined;
  const webkitEnterFullscreen = vi.fn();
  const requestFullscreen = vi.fn().mockResolvedValue(undefined);

  beforeEach(() => {
    vi.clearAllMocks();
    maxTouchPointsDescriptor = Object.getOwnPropertyDescriptor(window.navigator, 'maxTouchPoints');
    webkitEnterFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitEnterFullscreen');
    pictureInPictureEnabledDescriptor = Object.getOwnPropertyDescriptor(document, 'pictureInPictureEnabled');
    requestPictureInPictureDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'requestPictureInPicture');
    requestFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLDivElement.prototype, 'requestFullscreen');
    visibilityStateDescriptor = Object.getOwnPropertyDescriptor(document, 'visibilityState');

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
    vi.useRealTimers();

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

    if (visibilityStateDescriptor) {
      Object.defineProperty(document, 'visibilityState', visibilityStateDescriptor);
    }

    if (maxTouchPointsDescriptor) {
      Object.defineProperty(window.navigator, 'maxTouchPoints', maxTouchPointsDescriptor);
    }

    vi.restoreAllMocks();
  });

  it('uses native video fullscreen on touch devices when native HLS is preferred', async () => {
    const props = {
      src: 'http://example.com/playlist.m3u8',
      autoStart: true
    } as V3PlayerProps;
    const { container } = render(<V3Player {...props} />);

    await waitFor(() => {
      expect(Hls).not.toHaveBeenCalled();
    });

    const fullscreenButton = await screen.findByRole('button', { name: /fullscreen/i });
    const video = container.querySelector('video') as HTMLVideoElement;
    const seekableStart = 90;
    const seekableEnd = 120;
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => 1
    });
    Object.defineProperty(video, 'videoWidth', {
      configurable: true,
      get: () => 1920
    });
    Object.defineProperty(video, 'videoHeight', {
      configurable: true,
      get: () => 1080
    });
    Object.defineProperty(video, 'seekable', {
      configurable: true,
      get: () => ({
        length: seekableEnd > seekableStart ? 1 : 0,
        start: () => seekableStart,
        end: () => seekableEnd,
      })
    });
    fireEvent.click(fullscreenButton);

    expect(requestFullscreen).not.toHaveBeenCalled();
    expect(webkitEnterFullscreen).toHaveBeenCalledTimes(1);
    expect(video.controls).toBe(true);
    expect(screen.queryByRole('button', { name: /player\.dvrMode/i })).not.toBeInTheDocument();
  });

  it('defers native fullscreen on touch devices until metadata is available', async () => {
    let readyState = 0;
    let seekableStart = 0;
    let seekableEnd = 0;
    const props = {
      src: 'http://example.com/playlist.m3u8',
      autoStart: true
    } as V3PlayerProps;
    const { container } = render(<V3Player {...props} />);

    await waitFor(() => {
      expect(Hls).not.toHaveBeenCalled();
    });

    const video = container.querySelector('video') as HTMLVideoElement;
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => readyState
    });
    Object.defineProperty(video, 'videoWidth', {
      configurable: true,
      get: () => (readyState >= 1 ? 1920 : 0)
    });
    Object.defineProperty(video, 'videoHeight', {
      configurable: true,
      get: () => (readyState >= 1 ? 1080 : 0)
    });
    Object.defineProperty(video, 'seekable', {
      configurable: true,
      get: () => ({
        length: seekableEnd > seekableStart ? 1 : 0,
        start: () => seekableStart,
        end: () => seekableEnd,
      })
    });

    const fullscreenButton = await screen.findByRole('button', { name: /fullscreen/i });
    fireEvent.click(fullscreenButton);

    expect(webkitEnterFullscreen).not.toHaveBeenCalled();

    readyState = 1;
    fireEvent.loadedMetadata(video);

    expect(webkitEnterFullscreen).not.toHaveBeenCalled();

    seekableStart = 90;
    seekableEnd = 120;
    fireEvent.progress(video);

    expect(webkitEnterFullscreen).toHaveBeenCalledTimes(1);
    expect(video.controls).toBe(true);
  });

  it('keeps mute controls but hides the volume slider on mobile WebKit when the native HLS path is active', async () => {
    const props = {
      src: 'http://example.com/playlist.m3u8',
      autoStart: true
    } as V3PlayerProps;
    render(<V3Player {...props} />);

    await waitFor(() => {
      expect(Hls).not.toHaveBeenCalled();
    });

    expect(screen.queryByRole('slider')).not.toBeInTheDocument();
    expect(await screen.findByRole('button', { name: /unmute|mute/i })).toBeInTheDocument();
    expect(screen.getByText(/use device buttons/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /player\.pipLabel/i })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /fullscreen/i })).toBeInTheDocument();
  });

  it('collapses the inline bridge deck into idle mode on touch devices after the idle timeout', async () => {
    vi.useFakeTimers();

    const props = {
      src: 'http://example.com/playlist.m3u8',
      autoStart: true
    } as V3PlayerProps;
    const { container } = render(<V3Player {...props} />);

    expect(screen.getByRole('button', { name: /fullscreen/i })).toBeInTheDocument();

    const player = container.firstElementChild as HTMLElement;
    expect(player.className).not.toContain(styles.userIdle);

    await act(async () => {
      vi.advanceTimersByTime(4000);
    });

    expect(player.className).toContain(styles.userIdle);
    expect(screen.getByRole('button', { name: /fullscreen/i })).toBeInTheDocument();
  });

  it('resumes native inline playback after lock and unlock on touch devices', async () => {
    let visibilityState: DocumentVisibilityState = 'visible';
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => visibilityState,
    });

    let paused = false;
    const playSpy = vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(function () {
      paused = false;
      return Promise.resolve();
    });
    const pauseSpy = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(function () {
      paused = true;
    });

    const props = {
      src: 'http://example.com/playlist.m3u8',
      autoStart: true
    } as V3PlayerProps;
    const { container } = render(<V3Player {...props} />);

    await waitFor(() => {
      expect(Hls).not.toHaveBeenCalled();
    });

    const video = container.querySelector('video') as HTMLVideoElement;
    Object.defineProperty(video, 'paused', {
      configurable: true,
      get: () => paused,
    });

    visibilityState = 'hidden';
    fireEvent(document, new Event('visibilitychange'));
    expect(pauseSpy).toHaveBeenCalled();

    visibilityState = 'visible';
    fireEvent(document, new Event('visibilitychange'));

    await waitFor(() => {
      expect(playSpy).toHaveBeenCalled();
    });
  });

  it('reloads the native inline source when unlock resume stays stuck', async () => {
    let visibilityState: DocumentVisibilityState = 'visible';
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => visibilityState,
    });

    let paused = false;
    const readyState = 1;
    const playSpy = vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(() => Promise.resolve());
    const pauseSpy = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {
      paused = true;
    });
    const loadSpy = vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});

    const props = {
      src: 'http://example.com/playlist.m3u8',
      autoStart: true
    } as V3PlayerProps;
    const { container } = render(<V3Player {...props} />);

    await waitFor(() => {
      expect(Hls).not.toHaveBeenCalled();
    });

    vi.useFakeTimers();

    const video = container.querySelector('video') as HTMLVideoElement;
    Object.defineProperty(video, 'paused', {
      configurable: true,
      get: () => paused,
    });
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => readyState,
    });
    Object.defineProperty(video, 'currentSrc', {
      configurable: true,
      get: () => 'http://example.com/playlist.m3u8',
    });

    playSpy.mockClear();
    pauseSpy.mockClear();
    loadSpy.mockClear();

    visibilityState = 'hidden';
    fireEvent(document, new Event('visibilitychange'));
    expect(pauseSpy).toHaveBeenCalled();

    visibilityState = 'visible';
    fireEvent(document, new Event('visibilitychange'));

    await act(async () => {
      vi.advanceTimersByTime(1700);
    });
    fireEvent.loadedMetadata(video);

    expect(loadSpy).toHaveBeenCalled();
    expect(playSpy).toHaveBeenCalled();
  });

});
