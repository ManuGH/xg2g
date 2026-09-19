import { createRef } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type {
  PlaybackOrchestratorActions,
  V3PlayerViewState,
} from '../usePlaybackOrchestrator';
import { V3PlayerView } from './V3PlayerView';
import { buildPlayerViewState } from './playerViewStateModel';
import styles from './V3Player.module.css';

function createActions(): PlaybackOrchestratorActions {
  return {
    stopStream: vi.fn().mockResolvedValue(undefined),
    retry: vi.fn().mockResolvedValue(undefined),
    seekBy: vi.fn(),
    seekTo: vi.fn(),
    seekToLiveEdge: vi.fn(),
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
    changeProfile: vi.fn(),
    changeAudioTrack: vi.fn(),
  };
}

function createViewState(overrides: Partial<V3PlayerViewState> = {}): V3PlayerViewState {
  return {
    channelName: 'Das Erste HD',
    programmeTitle: null,
    programmeDesc: null,
    useOverlayLayout: false,
    userIdle: false,
    showCloseButton: false,
    closeButtonLabel: 'Close player',
    explicitProfile: 'auto',
    audioTracks: [],
    activeAudioTrack: -1,
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
    channelLogoUrl: null,
  showSpinnerCard: false,
    useNativeBufferingSafeOverlay: false,
    overlayStatusLabel: 'Buffering',
    overlayStatusState: 'live',
    spinnerEyebrow: 'Live startup',
    spinnerLabel: 'Preparing stream',
    spinnerSupport: 'This can take a moment.',
    startupPhaseSteps: [
      { key: 'connect', label: 'Connect', state: 'done' as const },
      { key: 'transcode', label: 'Transcode', state: 'active' as const },
      { key: 'buffer', label: 'Buffer', state: 'pending' as const },
    ],
    startupProgressPercent: 58,
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
    ttffBadgeLabel: null,
    ttffTitle: null,
    seekableStart: 0,
    seekableEnd: 0,
    startTimeDisplay: '00:00',
    endTimeDisplay: '00:00',
    currentPositionDisplay: '00:00',
    dvrPreviewBaseUrl: null,
    dvrPreviewSegmentSeconds: 10,
    dvrPreviewWindowStartUnix: null,
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
    isMuted: false,
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

    fireEvent.click(screen.getByRole('button', { name: 'Back 15s' }));
    fireEvent.change(screen.getByRole('slider'), { target: { value: '15' } });
    fireEvent.click(screen.getByRole('button', { name: 'Go live' }));
    fireEvent.click(screen.getByRole('button', { name: 'Resume' }));
    fireEvent.click(screen.getByRole('button', { name: 'Start over' }));

    expect(actions.seekBy).toHaveBeenCalledWith(-15);
    expect(actions.seekTo).toHaveBeenNthCalledWith(1, 115);
    // The LIVE button now goes through seekToLiveEdge (lands behind the edge),
    // not a raw seekTo(seekableEnd) which stalled on the un-decodable boundary.
    expect(actions.seekToLiveEdge).toHaveBeenCalledTimes(1);
    expect(actions.resumeFrom).toHaveBeenCalledWith(42);
    expect(actions.startOver).toHaveBeenCalledTimes(1);
  });

  it('renders the now-playing programme title with the channel as eyebrow', () => {
    const actions = createActions();
    const viewState = createViewState({
      showPlaybackChrome: true,
      channelName: 'ServusTV HD',
      programmeTitle: 'Servus am Abend',
      programmeDesc: 'Hintergründig und informativ. Das bewegt Österreich heute.',
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

    expect(screen.getByText('Servus am Abend')).toBeInTheDocument();
    expect(screen.getByText('ServusTV HD')).toBeInTheDocument();
  });

  it('falls back to the channel name as the title when no programme is known', () => {
    const actions = createActions();
    const viewState = createViewState({
      showPlaybackChrome: true,
      channelName: 'ORF 1 HD',
      programmeTitle: null,
      programmeDesc: null,
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

    // Channel name shown once, as the title (no separate eyebrow when there is no programme).
    expect(screen.getAllByText('ORF 1 HD')).toHaveLength(1);
  });

  it('allows switching picture mode and applies vivid filter class', () => {
    localStorage.clear();
    const actions = createActions();
    const viewState = createViewState({
      showPlaybackChrome: true,
    });

    const { container } = render(
      <V3PlayerView
        containerRef={createRef<HTMLDivElement>()}
        videoRef={createRef<HTMLVideoElement>()}
        resumePrimaryActionRef={createRef<HTMLButtonElement>()}
        viewState={viewState}
        actions={actions}
      />
    );

    const video = container.querySelector('video')!;
    expect(video.className).not.toContain('pictureModeVivid');

    const button = screen.getByTitle('Bildmodus');
    fireEvent.click(button);

    const vividOption = screen.getByText('Brillant (TV • OLED-Punch)');
    fireEvent.click(vividOption);

    expect(video.className).toContain('pictureModeVivid');
    expect(localStorage.getItem('xg2g.player.pictureMode')).toBe('vivid');
  });

  it('renders topHeader in fullscreen when playing a recording even without close button', () => {
    const actions = createActions();
    const viewState = createViewState({
      showCloseButton: false,
      fullscreenActive: true,
      programmeTitle: 'Unser Charly',
    });

    const { container } = render(
      <V3PlayerView
        containerRef={createRef<HTMLDivElement>()}
        videoRef={createRef<HTMLVideoElement>()}
        resumePrimaryActionRef={createRef<HTMLButtonElement>()}
        viewState={viewState}
        actions={actions}
      />
    );

    expect(container.querySelector(`.${styles.topHeader}`)).not.toBeNull();
    expect(screen.getAllByText('Unser Charly').length).toBeGreaterThanOrEqual(1);
  });

  it('buildPlayerViewState disables overlay and close button in page layout mode', () => {
    const state = buildPlayerViewState({
      t: ((k: string, opts?: any) => opts?.defaultValue || k) as any,
      formatClock: ((s: number) => String(s)) as any,
      channel: undefined,
      playbackMode: 'VOD',
      liveNowPlaying: { title: null, desc: null },
      onClose: () => {},
      layoutMode: 'page',
      recordingTitle: 'Tatort',
      recordingDescription: 'Krimi aus Münster',
      isIdle: false,
      status: 'playing',
      showStats: false,
      showPlaybackChrome: true,
      isWebKitFullscreenActive: false,
      isFullscreen: false,
      prefersDesktopNativeFullscreen: false,
      supportsNativeFullscreen: true,
      mseAv1Readout: '',
      effectiveSessionId: null,
      sessionPlaybackTrace: null,
      traceId: '123',
      effectiveClientPath: null,
      effectiveRequestProfile: null,
      effectiveRequestedIntent: null,
      effectiveResolvedIntent: null,
      effectiveQualityRung: null,
      effectiveAudioQualityRung: null,
      effectiveVideoQualityRung: null,
      effectiveDegradedFrom: null,
      effectiveHostPressureBand: null,
      effectiveHostOverrideApplied: false,
      effectiveForcedIntent: null,
      effectiveOperatorMaxQualityRung: null,
      effectiveOperatorRuleName: null,
      effectiveOperatorRuleScope: null,
      effectiveClientFallbackDisabled: false,
      effectiveOperatorOverrideApplied: false,
      sourceProfileSummary: '-',
      effectiveTargetProfile: null,
      effectiveTargetProfileHash: null,
      ffmpegPlanSummary: '-',
      firstFrameLabel: '-',
      fallbackSummary: '-',
      stopSummary: '-',
      hostPressureSummary: '-',
      playbackObservability: null,
      showNativeBufferingMask: false,
      stats: {
        resolution: '1920x1080',
        bandwidth: 0,
        fps: 50,
        droppedFrames: 0,
        buffer: 0,
        bufferHealth: 0,
        latency: null,
        levelIndex: 0,
      },
      hlsRefCurrent: false,
      seekableStart: 0,
      seekableEnd: 100,
      currentPlaybackTime: 0,
      windowDuration: 100,
      hasSeekWindow: true,
      isCompactTouchLayout: false,
      currentPositionDisplay: '0:00',
      dvrPreviewBaseUrl: null,
      dvrPreviewSegmentSeconds: 10,
      dvrPreviewWindowStartUnix: null,
      useMinimalStartupChrome: false,
      showStartupOverlay: false,
      showSpinnerCard: false,
      useNativeBufferingSafeOverlay: false,
      overlayStatus: 'idle',
      spinnerLabel: '',
      spinnerSupport: '',
      startupPhaseSteps: [],
      startupProgressPercent: 0,
      startupElapsedSeconds: 0,
      autoStart: true,
      error: null,
      showErrorDetails: false,
      effectiveSessionLabel: '',
      isPlaying: true,
      ttffMetrics: null,
      startTimeDisplay: '0:00',
      endTimeDisplay: '1:40',
      relativePosition: 0,
      isLiveMode: false,
      isAtLiveEdge: false,
      recordingId: 'rec-1',
      sRef: '',
      startIntentInFlightRef: false,
      showDvrModeButton: false,
      canToggleFullscreen: true,
      canEnterNativeFullscreen: false,
      canToggleMute: true,
      isMuted: false,
      canAdjustVolume: true,
      volume: 1,
      canTogglePiP: false,
      isPip: false,
      showResumeOverlay: false,
      resumeState: null,
      explicitProfile: 'auto',
      audioTracks: [],
      activeAudioTrack: -1,
      durationSeconds: 100,
    });

    expect(state.useOverlayLayout).toBe(false);
    expect(state.showCloseButton).toBe(false);
    expect(state.programmeTitle).toBe('Tatort');
    expect(state.programmeDesc).toBe('Krimi aus Münster');
  });
});
