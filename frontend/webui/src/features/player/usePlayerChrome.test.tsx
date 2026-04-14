import { createRef, useRef, useState } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { usePlayerChrome } from './usePlayerChrome';
import type { HlsInstanceRef, PlayerStatus } from '../../types/v3-player';

function HookHarness({
  shouldForceNativeMobileHls,
  canUseDesktopWebKitFullscreen = () => false,
  playbackMode = 'LIVE',
  durationSeconds = null,
  canSeek = false,
}: {
  shouldForceNativeMobileHls: () => boolean;
  canUseDesktopWebKitFullscreen?: () => boolean;
  playbackMode?: 'LIVE' | 'VOD' | 'UNKNOWN';
  durationSeconds?: number | null;
  canSeek?: boolean;
}) {
  const containerRef = createRef<HTMLDivElement>();
  const videoRef = createRef<HTMLVideoElement>();
  const hlsRef = useRef<HlsInstanceRef>(null);
  const userPauseIntentRef = useRef(false);
  const lastDecodedRef = useRef(0);
  const [, setStatus] = useState<PlayerStatus>('idle');

  const chrome = usePlayerChrome({
    autoStart: true,
    containerRef,
    videoRef,
    hlsRef,
    userPauseIntentRef,
    lastDecodedRef,
    playbackMode,
    durationSeconds,
    canSeek,
    startUnix: null,
    setStatus,
    allowNativeFullscreen: true,
    shouldForceNativeMobileHls,
    canUseDesktopWebKitFullscreen,
  });

  return (
    <div ref={containerRef}>
      <video ref={videoRef} data-testid="player-video" />
      <button onClick={chrome.applyAutoplayMute} type="button">
        mute
      </button>
      <button onClick={() => void chrome.toggleFullscreen()} type="button">
        fullscreen
      </button>
      <button onClick={() => chrome.seekTo(600)} type="button">
        seek
      </button>
      <button onClick={() => chrome.seekWhenReady(42)} type="button">
        resume
      </button>
    </div>
  );
}

describe('usePlayerChrome', () => {
  let requestFullscreenDescriptor: PropertyDescriptor | undefined;
  let webkitEnterFullscreenDescriptor: PropertyDescriptor | undefined;
  let playDescriptor: PropertyDescriptor | undefined;

  beforeEach(() => {
    requestFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLDivElement.prototype, 'requestFullscreen');
    webkitEnterFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitEnterFullscreen');
    playDescriptor = Object.getOwnPropertyDescriptor(HTMLMediaElement.prototype, 'play');
  });

  afterEach(() => {
    if (requestFullscreenDescriptor) {
      Object.defineProperty(HTMLDivElement.prototype, 'requestFullscreen', requestFullscreenDescriptor);
    } else {
      delete (HTMLDivElement.prototype as any).requestFullscreen;
    }

    if (webkitEnterFullscreenDescriptor) {
      Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', webkitEnterFullscreenDescriptor);
    } else {
      delete (HTMLVideoElement.prototype as any).webkitEnterFullscreen;
    }

    if (playDescriptor) {
      Object.defineProperty(HTMLMediaElement.prototype, 'play', playDescriptor);
    } else {
      delete (HTMLMediaElement.prototype as any).play;
    }

    vi.restoreAllMocks();
  });

  it('keeps autoplay audio enabled on the touch WebKit path', () => {
    render(<HookHarness shouldForceNativeMobileHls={() => true} />);

    const video = screen.getByTestId('player-video') as HTMLVideoElement;
    video.muted = false;

    fireEvent.click(screen.getByRole('button', { name: 'mute' }));

    expect(video.muted).toBe(false);
  });

  it('still mutes autoplay when the touch WebKit path is not active', () => {
    render(<HookHarness shouldForceNativeMobileHls={() => false} />);

    const video = screen.getByTestId('player-video') as HTMLVideoElement;
    video.muted = false;

    fireEvent.click(screen.getByRole('button', { name: 'mute' }));

    expect(video.muted).toBe(true);
  });

  it('prefers container fullscreen over desktop WebKit fullscreen by default', async () => {
    const requestFullscreen = vi.fn().mockResolvedValue(undefined);
    const webkitEnterFullscreen = vi.fn();

    Object.defineProperty(HTMLDivElement.prototype, 'requestFullscreen', {
      configurable: true,
      value: requestFullscreen
    });
    Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
      configurable: true,
      value: webkitEnterFullscreen
    });

    render(
      <HookHarness
        shouldForceNativeMobileHls={() => false}
        canUseDesktopWebKitFullscreen={() => true}
      />
    );

    fireEvent.click(screen.getByRole('button', { name: 'fullscreen' }));

    await waitFor(() => {
      expect(requestFullscreen).toHaveBeenCalledTimes(1);
    });
    expect(webkitEnterFullscreen).not.toHaveBeenCalled();
  });

  it('preserves the resume click gesture until metadata is available', () => {
    const play = vi.fn().mockResolvedValue(undefined);

    Object.defineProperty(HTMLMediaElement.prototype, 'play', {
      configurable: true,
      value: play,
    });

    render(
      <HookHarness
        shouldForceNativeMobileHls={() => false}
        playbackMode="VOD"
        durationSeconds={3600}
        canSeek={true}
      />
    );

    const video = screen.getByTestId('player-video') as HTMLVideoElement;
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => 0,
    });
    video.currentTime = 0;

    fireEvent.click(screen.getByRole('button', { name: 'resume' }));

    expect(play).toHaveBeenCalledTimes(1);
    expect(video.currentTime).toBe(0);

    fireEvent(video, new Event('loadedmetadata'));

    expect(video.currentTime).toBe(42);
    expect(play.mock.calls.length).toBeGreaterThanOrEqual(1);
  });


  it('clamps VOD seeks to the browser-reported seekable window', () => {
    render(
      <HookHarness
        shouldForceNativeMobileHls={() => false}
        playbackMode="VOD"
        durationSeconds={3600}
        canSeek={true}
      />
    );

    const video = screen.getByTestId('player-video') as HTMLVideoElement;
    Object.defineProperty(video, 'seekable', {
      configurable: true,
      value: {
        length: 1,
        start: () => 120,
        end: () => 240,
      },
    });
    video.currentTime = 0;

    fireEvent(video, new Event('loadedmetadata'));
    fireEvent.click(screen.getByRole('button', { name: 'seek' }));

    expect(video.currentTime).toBe(240);
  });
});
