import { createRef } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type {
  PlaybackOrchestratorActions,
  V3PlayerViewState,
} from '../usePlaybackOrchestrator';
import { V3PlayerView } from './V3PlayerView';

function createActions(): PlaybackOrchestratorActions {
  return {
    stopStream: vi.fn().mockResolvedValue(undefined),
    retry: vi.fn().mockResolvedValue(undefined),
    seekBy: vi.fn(),
    seekTo: vi.fn(),
    togglePlayPause: vi.fn(),
    updateServiceRef: vi.fn(),
    submitServiceRef: vi.fn(),
    startStream: vi.fn(),
    enterDVRMode: vi.fn(),
    enterNativeFullscreen: vi.fn(),
    toggleFullscreen: vi.fn().mockResolvedValue(undefined),
    toggleMute: vi.fn(),
    changeVolume: vi.fn(),
    togglePiP: vi.fn().mockResolvedValue(undefined),
    toggleStats: vi.fn(),
    toggleErrorDetails: vi.fn(),
    resumeFrom: vi.fn(),
    startOver: vi.fn(),
  };
}

function createViewState(overrides: Partial<V3PlayerViewState> = {}): V3PlayerViewState {
  return {
    channelName: 'Das Erste HD',
    useOverlayLayout: false,
    userIdle: false,
    showCloseButton: false,
    closeButtonLabel: 'Close player',
    showStatsOverlay: false,
    statsTitle: 'Technical Stats',
    statusLabel: 'Status',
    statusChipLabel: 'Playing',
    statusChipState: 'live',
    statsRows: [],
    showNativeBufferingMask: false,
    hideVideoElement: false,
    showStartupBackdrop: false,
    showStartupOverlay: false,
    useNativeBufferingSafeOverlay: false,
    overlayStatusLabel: 'Buffering',
    overlayStatusState: 'live',
    spinnerEyebrow: 'Live startup',
    spinnerLabel: 'Preparing stream',
    spinnerSupport: 'This can take a moment.',
    startupElapsedLabel: 'Wait 1s',
    showOverlayStopAction: false,
    overlayStopLabel: 'Stop',
    videoClassName: '',
    autoPlay: false,
    error: null,
    showErrorDetails: false,
    errorRetryLabel: 'Retry',
    errorTelemetryRows: [],
    errorDetailToggleLabel: null,
    errorSessionLabel: 'Session: -',
    showPlaybackChrome: true,
    showSeekControls: false,
    seekBack15mLabel: 'Back 15m',
    seekBack60sLabel: 'Back 60s',
    seekBack15sLabel: 'Back 15s',
    seekForward15sLabel: 'Forward 15s',
    seekForward60sLabel: 'Forward 60s',
    seekForward15mLabel: 'Forward 15m',
    playPauseLabel: 'Play',
    playPauseIcon: '▶',
    seekableStart: 0,
    seekableEnd: 0,
    startTimeDisplay: '00:00',
    endTimeDisplay: '00:00',
    windowDuration: 0,
    relativePosition: 0,
    isLiveMode: false,
    isAtLiveEdge: false,
    liveButtonLabel: 'Go live',
    showServiceInput: false,
    serviceRef: '',
    showManualStartButton: false,
    manualStartLabel: 'Start stream',
    manualStartDisabled: false,
    showDvrModeButton: false,
    dvrModeLabel: 'DVR',
    showNativeFullscreenButton: false,
    nativeFullscreenTitle: 'Native fullscreen',
    nativeFullscreenLabel: 'Native',
    showFullscreenButton: false,
    fullscreenLabel: 'Fullscreen',
    fullscreenActive: false,
    showVolumeControls: false,
    audioToggleLabel: 'Mute',
    audioToggleIcon: '🔇',
    audioToggleActive: true,
    canAdjustVolume: false,
    volume: 1,
    deviceVolumeHint: 'Use device buttons',
    showPipButton: false,
    pipTitle: 'Picture in picture',
    pipLabel: 'PiP',
    pipActive: false,
    statsLabel: 'Stats',
    statsActive: false,
    showStopButton: false,
    stopLabel: 'Stop',
    showResumeOverlay: false,
    resumeTitle: 'Resume',
    resumePrompt: 'Resume from 00:30?',
    resumeActionLabel: 'Resume',
    startOverLabel: 'Start over',
    resumePositionSeconds: null,
    playback: {
      durationSeconds: null,
    },
    ...overrides,
  };
}

describe('V3PlayerView', () => {
  it('renders retry/error details and forwards callbacks without domain logic', () => {
    const actions = createActions();
    const viewState = createViewState({
      error: {
        title: 'Playback failed',
        detail: 'requestId=req-1',
        retryable: true,
      },
      errorDetailToggleLabel: 'Show details',
      showStopButton: true,
    });

    render(
      <V3PlayerView
        containerRef={createRef<HTMLDivElement>()}
        videoRef={createRef<HTMLVideoElement>()}
        resumePrimaryActionRef={createRef<HTMLButtonElement>()}
        viewState={viewState}
        actions={actions}
      />
    );

    expect(screen.getByRole('alert')).toHaveTextContent('Playback failed');

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    fireEvent.click(screen.getByRole('button', { name: 'Show details' }));
    fireEvent.click(screen.getByRole('button', { name: /stop/i }));

    expect(actions.retry).toHaveBeenCalledTimes(1);
    expect(actions.toggleErrorDetails).toHaveBeenCalledTimes(1);
    expect(actions.stopStream).toHaveBeenCalledTimes(1);
  });

  it('renders seek controls and resume overlay from the supplied view state', () => {
    const actions = createActions();
    const viewState = createViewState({
      showSeekControls: true,
      windowDuration: 120,
      relativePosition: 30,
      seekableStart: 100,
      seekableEnd: 220,
      isLiveMode: true,
      resumePositionSeconds: 42,
      showResumeOverlay: true,
    });

    render(
      <V3PlayerView
        containerRef={createRef<HTMLDivElement>()}
        videoRef={createRef<HTMLVideoElement>()}
        resumePrimaryActionRef={createRef<HTMLButtonElement>()}
        viewState={viewState}
        actions={actions}
      />
    );

    fireEvent.click(screen.getByRole('button', { name: 'Back 60s' }));
    fireEvent.change(screen.getByRole('slider'), { target: { value: '15' } });
    fireEvent.click(screen.getByRole('button', { name: 'Go live' }));
    fireEvent.click(screen.getByRole('button', { name: 'Resume' }));
    fireEvent.click(screen.getByRole('button', { name: 'Start over' }));

    expect(actions.seekBy).toHaveBeenCalledWith(-60);
    expect(actions.seekTo).toHaveBeenNthCalledWith(1, 115);
    expect(actions.seekTo).toHaveBeenNthCalledWith(2, 220);
    expect(actions.resumeFrom).toHaveBeenCalledWith(42);
    expect(actions.startOver).toHaveBeenCalledTimes(1);
  });
});
