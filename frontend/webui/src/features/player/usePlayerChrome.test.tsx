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
  mediaTitle = 'ProSieben HD',
  mediaSubtitle = 'xg2g',
  mediaArtworkUrl = 'https://example.com/logo.png',
}: {
  shouldForceNativeMobileHls: () => boolean;
  canUseDesktopWebKitFullscreen?: () => boolean;
  playbackMode?: 'LIVE' | 'VOD' | 'UNKNOWN';
  durationSeconds?: number | null;
  canSeek?: boolean;
  liveSeekWindow?: { start: number; end: number; liveEdge: number | null } | null;
  mediaTitle?: string | null;
  mediaSubtitle?: string | null;
  mediaArtworkUrl?: string | null;
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
    mediaTitle,
    mediaSubtitle,
    mediaArtworkUrl,
  });

  return (
    <div ref={containerRef}>
      <video ref={videoRef} data-testid="player-video" />
      <output data-testid="has-seek-window">{String(chrome.hasSeekWindow)}</output>
      <output data-testid="has-live-dvr-window">{String(chrome.hasLiveDvrWindow)}</output>
      <output data-testid="is-live-mode">{String(chrome.isLiveMode)}</output>
      <output data-testid="is-at-live-edge">{String(chrome.isAtLiveEdge)}</output>
      <output data-testid="show-dvr-button">{String(chrome.showDvrModeButton)}</output>
      <output data-testid="native-pending">{String(chrome.nativeFullscreenPending)}</output>
      <button onClick={chrome.applyAutoplayMute} type="button">
        mute
      </button>
      <button onClick={() => void chrome.toggleFullscreen()} type="button">
        fullscreen
      </button>
      <button onClick={chrome.enterNativeFullscreen} type="button">
        native
      </button>
      <button onClick={chrome.togglePlayPause} type="button">
        playpause
      </button>
      <button onClick={chrome.seekToLiveEdge} type="button">
        golive
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
  let mediaSessionDescriptor: PropertyDescriptor | undefined;
  let originalMediaMetadata: typeof MediaMetadata | undefined;

  beforeEach(() => {
    requestFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLDivElement.prototype, 'requestFullscreen');
    webkitEnterFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitEnterFullscreen');
    webkitExitFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitExitFullscreen');
    fullscreenElementDescriptor = Object.getOwnPropertyDescriptor(Document.prototype, 'fullscreenElement');
    exitFullscreenDescriptor = Object.getOwnPropertyDescriptor(Document.prototype, 'exitFullscreen');
    mediaSessionDescriptor = Object.getOwnPropertyDescriptor(window.navigator, 'mediaSession');
    originalMediaMetadata = (window as typeof window & { MediaMetadata?: typeof MediaMetadata }).MediaMetadata;

    const mediaSessionState: {
      metadata: MediaMetadata | null;
      playbackState: MediaSessionPlaybackState;
      handlers: Partial<Record<MediaSessionAction, MediaSessionActionHandler | null>>;
    } = {
      metadata: null,
      playbackState: 'none',
      handlers: {},
    };

    class FakeMediaMetadata {
      title: string;
      artist: string;
      album: string;
      artwork: MediaImage[] | undefined;

      constructor(init: MediaMetadataInit = {}) {
        this.title = init.title ?? '';
        this.artist = init.artist ?? '';
        this.album = init.album ?? '';
        this.artwork = init.artwork;
      }
    }

    (window as typeof window & { MediaMetadata?: typeof MediaMetadata }).MediaMetadata = FakeMediaMetadata as unknown as typeof MediaMetadata;
    Object.defineProperty(window.navigator, 'mediaSession', {
      configurable: true,
      value: {
        get metadata() {
          return mediaSessionState.metadata;
        },
        set metadata(value: MediaMetadata | null) {
          mediaSessionState.metadata = value;
        },
        get playbackState() {
          return mediaSessionState.playbackState;
        },
        set playbackState(value: MediaSessionPlaybackState) {
          mediaSessionState.playbackState = value;
        },
        setActionHandler(action: MediaSessionAction, handler: MediaSessionActionHandler | null) {
          mediaSessionState.handlers[action] = handler;
        },
        setPositionState: vi.fn(),
      },
    });
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

    if (mediaSessionDescriptor) {
      Object.defineProperty(window.navigator, 'mediaSession', mediaSessionDescriptor);
    } else {
      delete (window.navigator as any).mediaSession;
    }

    if (originalMediaMetadata) {
      (window as typeof window & { MediaMetadata?: typeof MediaMetadata }).MediaMetadata = originalMediaMetadata;
    } else {
      delete (window as any).MediaMetadata;
    }

    vi.restoreAllMocks();
  });

  it('mutes autoplay on the touch WebKit path too', () => {
    render(<HookHarness shouldForceNativeMobileHls={() => true} playbackMode="VOD" durationSeconds={1800} />);

    const video = screen.getByTestId('player-video') as HTMLVideoElement;
    video.muted = false;

    fireEvent.click(screen.getByRole('button', { name: 'mute' }));

    expect(video.muted).toBe(true);
  });

  it('keeps autoplay mute when fullscreen is requested before an explicit play tap', async () => {
    const webkitEnterFullscreen = vi.fn();

    Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
      configurable: true,
      value: webkitEnterFullscreen
    });

    render(<HookHarness shouldForceNativeMobileHls={() => true} playbackMode="VOD" durationSeconds={1800} />);

    const video = screen.getByTestId('player-video') as HTMLVideoElement;
    video.muted = false;
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => 1,
    });

    fireEvent.click(screen.getByRole('button', { name: 'mute' }));
    expect(video.muted).toBe(true);

    fireEvent.click(screen.getByRole('button', { name: 'fullscreen' }));

    await waitFor(() => {
      expect(webkitEnterFullscreen).toHaveBeenCalledTimes(1);
    });
    expect(video.muted).toBe(true);
  });

  it('defers desktop native fullscreen for live DVR until Safari exposes a seekable window', async () => {
    const webkitEnterFullscreen = vi.fn();

    Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
      configurable: true,
      value: webkitEnterFullscreen,
    });

    render(
      <HookHarness
        shouldForceNativeMobileHls={() => false}
        canUseDesktopWebKitFullscreen={() => true}
        playbackMode="LIVE"
        liveSeekWindow={{ start: 0, end: 120, liveEdge: 120 }}
      />
    );

    const video = screen.getByTestId('player-video') as HTMLVideoElement;
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => 1,
    });

    fireEvent.click(screen.getByRole('button', { name: 'native' }));

    expect(webkitEnterFullscreen).not.toHaveBeenCalled();
    expect(screen.getByTestId('native-pending')).toHaveTextContent('true');

    Object.defineProperty(video, 'seekable', {
      configurable: true,
      get: () => ({
        length: 1,
        start: () => 0,
        end: () => 36,
      }),
    });

    fireEvent(video, new Event('timeupdate'));

    await waitFor(() => {
      expect(webkitEnterFullscreen).toHaveBeenCalledTimes(1);
    });
    expect(screen.getByTestId('native-pending')).toHaveTextContent('false');
  });

  it('enters desktop native fullscreen immediately for VOD playback', () => {
    const webkitEnterFullscreen = vi.fn();

    Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
      configurable: true,
      value: webkitEnterFullscreen,
    });

    render(
      <HookHarness
        shouldForceNativeMobileHls={() => false}
        canUseDesktopWebKitFullscreen={() => true}
        playbackMode="VOD"
        durationSeconds={1800}
      />
    );

    const video = screen.getByTestId('player-video') as HTMLVideoElement;
    Object.defineProperty(video, 'readyState', {
      configurable: true,
      get: () => 1,
    });

    fireEvent.click(screen.getByRole('button', { name: 'native' }));

    expect(webkitEnterFullscreen).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('native-pending')).toHaveTextContent('false');
  });

  it('clears autoplay mute when play is tapped on the native touch path', () => {
    render(<HookHarness shouldForceNativeMobileHls={() => true} />);

    const video = screen.getByTestId('player-video') as HTMLVideoElement;
    const playSpy = vi.spyOn(video, 'play').mockResolvedValue(undefined);
    Object.defineProperty(video, 'paused', {
      configurable: true,
      get: () => true,
    });
    video.muted = false;

    fireEvent.click(screen.getByRole('button', { name: 'mute' }));
    expect(video.muted).toBe(true);

    fireEvent.click(screen.getByRole('button', { name: 'playpause' }));

    expect(video.muted).toBe(false);
    expect(playSpy).toHaveBeenCalledTimes(1);
  });

  it('publishes media session metadata for lock screen playback', () => {
    render(
      <HookHarness
        shouldForceNativeMobileHls={() => true}
        mediaTitle="ProSieben HD"
        mediaSubtitle="Live TV"
        mediaArtworkUrl="https://example.com/pro7.png"
      />
    );

    const mediaSession = window.navigator.mediaSession as MediaSession;
    expect(mediaSession.metadata?.title).toBe('ProSieben HD');
    expect(mediaSession.metadata?.artist).toBe('Live TV');
    expect(mediaSession.playbackState).toBe('paused');
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

  it('seekToLiveEdge lands a safety margin behind the edge, never on it', async () => {
    render(
      <HookHarness
        shouldForceNativeMobileHls={() => false}
        liveSeekWindow={{ start: 0, end: 120, liveEdge: 120 }}
      />
    );

    const video = screen.getByTestId('player-video') as HTMLVideoElement;
    let currentTime = 30;
    Object.defineProperty(video, 'currentTime', {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
      },
    });
    Object.defineProperty(video, 'paused', { configurable: true, get: () => false });
    // Establish the seekable window (end=120).
    Object.defineProperty(video, 'seekable', {
      configurable: true,
      get: () => ({ length: 1, start: () => 0, end: () => 120 }),
    });
    fireEvent(video, new Event('loadedmetadata'));

    fireEvent.click(screen.getByRole('button', { name: 'golive' }));

    // 120 - 6 (safety gap) = 114; must NOT be the exact edge (120).
    expect(currentTime).toBe(114);
    expect(currentTime).toBeLessThan(120);
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
