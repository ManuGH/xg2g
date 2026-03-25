import { createRef, useRef, useState } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { usePlayerChrome } from './usePlayerChrome';
import type { HlsInstanceRef, PlayerStatus } from '../../types/v3-player';

function HookHarness({ shouldForceNativeMobileHls }: { shouldForceNativeMobileHls: () => boolean }) {
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
    canUseDesktopWebKitFullscreen: () => false,
  });

  return (
    <div ref={containerRef}>
      <video ref={videoRef} data-testid="player-video" />
      <button onClick={chrome.applyAutoplayMute} type="button">
        mute
      </button>
    </div>
  );
}

describe('usePlayerChrome', () => {
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
});
