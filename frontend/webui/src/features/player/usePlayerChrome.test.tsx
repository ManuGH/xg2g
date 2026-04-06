import { createRef, useRef, useState } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { usePlayerChrome } from './usePlayerChrome';
import type { HlsInstanceRef, PlayerStatus } from '../../types/v3-player';

function HookHarness({
  shouldForceNativeMobileHls,
  canUseDesktopWebKitFullscreen = () => false
}: {
  shouldForceNativeMobileHls: () => boolean;
  canUseDesktopWebKitFullscreen?: () => boolean;
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
    playbackMode: 'LIVE',
    durationSeconds: null,
    canSeek: false,
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
    </div>
  );
}

describe('usePlayerChrome', () => {
  let requestFullscreenDescriptor: PropertyDescriptor | undefined;
  let webkitEnterFullscreenDescriptor: PropertyDescriptor | undefined;

  beforeEach(() => {
    requestFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLDivElement.prototype, 'requestFullscreen');
    webkitEnterFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitEnterFullscreen');
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
});
