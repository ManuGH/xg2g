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
  liveSeekWindow = null,
}: {
  shouldForceNativeMobileHls: () => boolean;
  canUseDesktopWebKitFullscreen?: () => boolean;
  playbackMode?: 'LIVE' | 'VOD' | 'UNKNOWN';
  durationSeconds?: number | null;
  canSeek?: boolean;
  liveSeekWindow?: { start: number; end: number; liveEdge: number | null } | null;
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
    liveSeekWindow,
    setStatus,
    allowNativeFullscreen: true,
    shouldForceNativeMobileHls,
    canUseDesktopWebKitFullscreen,
  });

  return (
    <div ref={containerRef}>
      <video ref={videoRef} data-testid="player-video" />
      <output data-testid="has-seek-window">{String(chrome.hasSeekWindow)}</output>
      <output data-testid="has-live-dvr-window">{String(chrome.hasLiveDvrWindow)}</output>
      <output data-testid="is-live-mode">{String(chrome.isLiveMode)}</output>
      <output data-testid="is-at-live-edge">{String(chrome.isAtLiveEdge)}</output>
      <output data-testid="show-dvr-button">{String(chrome.showDvrModeButton)}</output>
      <button onClick={chrome.applyAutoplayMute} type="button">
        mute
      </button>
      <button onClick={() => void chrome.toggleFullscreen()} type="button">
        fullscreen
      </button>
    </div>
  );
}

describe('usePlayerChrome', () => {
  let requestFullscreenDescriptor: PropertyDescriptor | undefined;
  let webkitEnterFullscreenDescriptor: PropertyDescriptor | undefined;
  let webkitExitFullscreenDescriptor: PropertyDescriptor | undefined;
  let fullscreenElementDescriptor: PropertyDescriptor | undefined;
  let exitFullscreenDescriptor: PropertyDescriptor | undefined;

  beforeEach(() => {
    requestFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLDivElement.prototype, 'requestFullscreen');
    webkitEnterFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitEnterFullscreen');
    webkitExitFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitExitFullscreen');
    fullscreenElementDescriptor = Object.getOwnPropertyDescriptor(Document.prototype, 'fullscreenElement');
    exitFullscreenDescriptor = Object.getOwnPropertyDescriptor(Document.prototype, 'exitFullscreen');
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

    if (webkitExitFullscreenDescriptor) {
      Object.defineProperty(HTMLVideoElement.prototype, 'webkitExitFullscreen', webkitExitFullscreenDescriptor);
    } else {
      delete (HTMLVideoElement.prototype as any).webkitExitFullscreen;
    }

    if (fullscreenElementDescriptor) {
      Object.defineProperty(Document.prototype, 'fullscreenElement', fullscreenElementDescriptor);
    } else {
      delete (Document.prototype as any).fullscreenElement;
    }

    if (exitFullscreenDescriptor) {
      Object.defineProperty(Document.prototype, 'exitFullscreen', exitFullscreenDescriptor);
    } else {
      delete (Document.prototype as any).exitFullscreen;
    }

    vi.restoreAllMocks();
  });

  it('mutes autoplay on the touch WebKit path too', () => {
    render(<HookHarness shouldForceNativeMobileHls={() => true} />);

    const video = screen.getByTestId('player-video') as HTMLVideoElement;
    video.muted = false;

    fireEvent.click(screen.getByRole('button', { name: 'mute' }));

    expect(video.muted).toBe(true);
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

  it('exits root fullscreen when playback already owns document fullscreen', async () => {
    let currentFullscreenElement: Element | null = document.createElement('div');
    const exitFullscreen = vi.fn().mockImplementation(async () => {
      currentFullscreenElement = null;
    });

    Object.defineProperty(Document.prototype, 'exitFullscreen', {
      configurable: true,
      value: exitFullscreen
    });
    Object.defineProperty(Document.prototype, 'fullscreenElement', {
      configurable: true,
      get: () => currentFullscreenElement
    });

    currentFullscreenElement = document.documentElement;

    render(<HookHarness shouldForceNativeMobileHls={() => false} />);

    fireEvent.keyDown(window, { key: 'f' });

    await waitFor(() => {
      expect(exitFullscreen).toHaveBeenCalledTimes(1);
    });
  });

  it('exits foreign fullscreen before promoting to the player container', async () => {
    let currentFullscreenElement: Element | null = document.createElement('div');
    const requestFullscreen = vi.fn().mockResolvedValue(undefined);
    const exitFullscreen = vi.fn().mockImplementation(async () => {
      currentFullscreenElement = null;
    });

    Object.defineProperty(HTMLDivElement.prototype, 'requestFullscreen', {
      configurable: true,
      value: requestFullscreen
    });
    Object.defineProperty(Document.prototype, 'exitFullscreen', {
      configurable: true,
      value: exitFullscreen
    });
    Object.defineProperty(Document.prototype, 'fullscreenElement', {
      configurable: true,
      get: () => currentFullscreenElement
    });

    render(<HookHarness shouldForceNativeMobileHls={() => false} />);

    fireEvent.keyDown(window, { key: 'f' });

    await waitFor(() => {
      expect(exitFullscreen).toHaveBeenCalledTimes(1);
      expect(requestFullscreen).toHaveBeenCalledTimes(1);
    });
  });

  it('exits native WebKit fullscreen when f is pressed again', async () => {
    const webkitExitFullscreen = vi.fn();

    Object.defineProperty(HTMLVideoElement.prototype, 'webkitExitFullscreen', {
      configurable: true,
      value: webkitExitFullscreen
    });

    render(<HookHarness shouldForceNativeMobileHls={() => false} />);

    const video = screen.getByTestId('player-video') as HTMLVideoElement & { webkitDisplayingFullscreen?: boolean };
    video.webkitDisplayingFullscreen = true;

    fireEvent.keyDown(window, { key: 'f' });

    await waitFor(() => {
      expect(webkitExitFullscreen).toHaveBeenCalledTimes(1);
    });
  });

  it('defaults touch live DVR slightly behind the live edge', async () => {
    render(
      <HookHarness
        shouldForceNativeMobileHls={() => true}
        liveSeekWindow={{ start: 90, end: 120, liveEdge: 120 }}
      />
    );

    const video = screen.getByTestId('player-video') as HTMLVideoElement;
    let currentTime = 119;
    Object.defineProperty(video, 'currentTime', {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
      },
    });
    fireEvent(video, new Event('loadedmetadata'));

    await waitFor(() => {
      expect(screen.getByTestId('has-seek-window')).toHaveTextContent('true');
      expect(screen.getByTestId('has-live-dvr-window')).toHaveTextContent('true');
      expect(screen.getByTestId('is-live-mode')).toHaveTextContent('true');
      expect(screen.getByTestId('is-at-live-edge')).toHaveTextContent('false');
      expect(screen.getByTestId('show-dvr-button')).toHaveTextContent('true');
    });

    expect(currentTime).toBeLessThan(119);
  });
});
