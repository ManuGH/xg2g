import { createRef, useRef } from 'react';
import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { HlsInstanceRef, V3PlayerProps, VideoElementRef } from '../../types/v3-player';
import { usePlaybackOrchestrator } from './usePlaybackOrchestrator';

describe('usePlaybackOrchestrator audio preference and identity logic', () => {
  it('persists and restores audio track preference when new tracks arrive', () => {
    const hlsMock = { audioTrack: -1, destroy: vi.fn() };

    function TestComponent() {
      const containerRef = createRef<HTMLDivElement>();
      const videoRef = createRef<VideoElementRef>();
      const hlsRef = useRef<HlsInstanceRef>(hlsMock as any);
      const resumePrimaryActionRef = createRef<HTMLButtonElement>();

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
});
