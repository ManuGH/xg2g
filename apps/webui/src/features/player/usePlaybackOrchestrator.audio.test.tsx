import { createRef, useRef } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { HlsInstanceRef, V3PlayerProps, VideoElementRef } from '../../types/v3-player';
import { usePlaybackOrchestrator } from './usePlaybackOrchestrator';

describe('usePlaybackOrchestrator audio preference and identity logic', () => {
  it('persists and restores audio track preference when new tracks arrive', () => {
    const hlsMock = { audioTrack: -1, destroy: vi.fn() };

    function TestComponent() {
      const containerRef = useRef<HTMLDivElement>(null);
      const videoRef = useRef<VideoElementRef>(null);
      const hlsRef = useRef<HlsInstanceRef>(hlsMock as any);
      const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

      const { viewState, actions } = usePlaybackOrchestrator(
        { autoStart: false, revealHoldMs: 0 } as unknown as V3PlayerProps,
        { containerRef, videoRef, hlsRef, resumePrimaryActionRef }
      );

      return (
        <div>
          <span data-testid="active">{viewState.activeAudioTrack}</span>
          <span data-testid="tracks-count">{viewState.audioTracks.length}</span>
          <button
            onClick={() => actions.changeAudioTrack(1)}
          >
            switch-1
          </button>
        </div>
      );
    }

    render(<TestComponent />);
    expect(screen.getByTestId('active')).toHaveTextContent('-1');
  });

  it('cleans up addtrack, removetrack, and change listeners from HTMLVideoElement.audioTracks on unmount', () => {
    const removeEventListenerMock = vi.fn();
    const addEventListenerMock = vi.fn();
    const fakeAudioTracks = {
      length: 1,
      0: { id: 'de-native', language: 'de', label: 'Deutsch', enabled: true },
      addEventListener: addEventListenerMock,
      removeEventListener: removeEventListenerMock,
    };

    const videoElement = document.createElement('video');
    videoElement.setAttribute('src', 'https://example.invalid/live.m3u8');
    Object.defineProperty(videoElement, 'audioTracks', {
      value: fakeAudioTracks,
      configurable: true,
    });

    const videoRef = { current: videoElement as unknown as VideoElementRef };
    const containerRef = createRef<HTMLDivElement>();
    const hlsRef = { current: null };
    const resumePrimaryActionRef = createRef<HTMLButtonElement>();

    function VideoTestComponent() {
      usePlaybackOrchestrator(
        { autoStart: false, revealHoldMs: 0 } as unknown as V3PlayerProps,
        { containerRef, videoRef, hlsRef, resumePrimaryActionRef }
      );
      return <div />;
    }

    const { unmount } = render(<VideoTestComponent />);
    expect(addEventListenerMock).toHaveBeenCalledWith('addtrack', expect.any(Function));
    expect(addEventListenerMock).toHaveBeenCalledWith('removetrack', expect.any(Function));
    expect(addEventListenerMock).toHaveBeenCalledWith('change', expect.any(Function));

    unmount();
    expect(removeEventListenerMock).toHaveBeenCalledWith('addtrack', expect.any(Function));
    expect(removeEventListenerMock).toHaveBeenCalledWith('removetrack', expect.any(Function));
    expect(removeEventListenerMock).toHaveBeenCalledWith('change', expect.any(Function));
  });

  it('clears stale native audio tracks when playback stops', async () => {
    const fakeAudioTracks = {
      length: 2,
      0: { id: 'de-native', language: 'de', label: 'Deutsch', enabled: true },
      1: { id: 'en-native', language: 'en', label: 'English', enabled: false },
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    };
    const videoElement = document.createElement('video');
    videoElement.setAttribute('src', 'https://example.invalid/live-with-audio.m3u8');
    Object.defineProperty(videoElement, 'audioTracks', {
      value: fakeAudioTracks,
      configurable: true,
    });
    const videoRef = { current: videoElement as unknown as VideoElementRef };
    const containerRef = createRef<HTMLDivElement>();
    const hlsRef = { current: null };
    const resumePrimaryActionRef = createRef<HTMLButtonElement>();

    function VideoTestComponent() {
      const { viewState, actions } = usePlaybackOrchestrator(
        { autoStart: false, revealHoldMs: 0 } as unknown as V3PlayerProps,
        { containerRef, videoRef, hlsRef, resumePrimaryActionRef }
      );
      return (
        <div>
          <span data-testid="tracks-count">{viewState.audioTracks.length}</span>
          <button onClick={() => void actions.stopStream()}>stop</button>
        </div>
      );
    }

    render(<VideoTestComponent />);
    await waitFor(() => {
      expect(screen.getByTestId('tracks-count')).toHaveTextContent('2');
    });
    fireEvent.click(screen.getByRole('button', { name: 'stop' }));
    await waitFor(() => {
      expect(screen.getByTestId('tracks-count')).toHaveTextContent('0');
    });
  });
});
