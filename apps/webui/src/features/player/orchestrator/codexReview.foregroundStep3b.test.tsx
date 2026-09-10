// Copy beside playbackController.ts to run. Independent review counterexamples.
import React, { useRef, useState } from 'react';
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type Hls from 'hls.js';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { createRecoveryLadderState } from './recoveryLadder';
import { useForegroundRecovery } from './useForegroundRecovery';
import { startResumePlaybackRecovery } from './resumePlaybackRecovery';
import { buildPlaybackFailure } from './playbackMachine';
import type { PlayerStatus } from '../../../types/v3-player';

const controllers: PlaybackController[] = [];
function controllerAt(epoch = 1) {
  const controller = createPlaybackController({
    createInitialState: () => ({
      epoch: { playback: epoch, session: 0 }, traceId: '-', status: 'playing',
      playbackMode: 'LIVE', vodStreamMode: null, activeHlsEngine: null,
      durationSeconds: null, canSeek: false, startUnix: null,
      sessionPhase: 'ready', mediaPhase: 'playing', contract: null, failure: null,
      lastAdvisory: null, explicitProfilePinned: false, hasSessionIntent: true,
      recovery: createRecoveryLadderState(), leaseExpiresAt: null, connectionLost: false,
    }),
  });
  controllers.push(controller);
  return controller;
}

function Player({ controller, visible, elementKey = 'a', refreshSetter = false }: {
  controller: PlaybackController; visible: boolean; elementKey?: string; refreshSetter?: boolean;
}) {
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const hlsRef = useRef<Hls | null>(null);
  const paused = useRef(false);
  const [, stableSetter] = useState<PlayerStatus>('playing');
  // refreshSetter models the existing tests; default models production's stable setter.
  const setter = refreshSetter
    ? (value: React.SetStateAction<PlayerStatus>) => stableSetter(value)
    : stableSetter;
  useForegroundRecovery({
    controller, videoRef, hlsRef, isEligible: true, isDocumentVisible: visible,
    target: { kind: 'live', serviceRef: 'channel-a' }, userPauseIntentRef: paused,
    setStatus: setter,
  });
  return <video key={elementKey} ref={videoRef} />;
}

describe('Step 3b independent review regressions', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined);
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
    vi.clearAllMocks();
  });
  afterEach(() => {
    cleanup();
    controllers.splice(0).forEach((c) => c.dispose());
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('attaches a video mounted after render with stable production-style callbacks', () => {
    const controller = controllerAt();
    const view = render(<Player controller={controller} visible={false} />);
    view.rerender(<Player controller={controller} visible />);
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalledTimes(1);
    expect(controller.getActiveForegroundOperationId()).not.toBeNull();
  });

  it('cancels the old nudge when React replaces the actual video element', () => {
    const controller = controllerAt();
    const view = render(<Player controller={controller} visible={false} refreshSetter />);
    view.rerender(<Player controller={controller} visible refreshSetter />);
    const oldVideo = view.container.querySelector('video')!;
    const oldPlay = vi.spyOn(oldVideo, 'play');
    oldPlay.mockClear();
    expect(controller.getActiveForegroundOperationId()).not.toBeNull();

    view.rerender(<Player controller={controller} visible elementKey="b" refreshSetter />);
    expect(view.container.querySelector('video')).not.toBe(oldVideo);
    expect(oldVideo.isConnected).toBe(false);
    act(() => { vi.advanceTimersByTime(400); });
    expect(oldPlay).not.toHaveBeenCalled();
    expect(controller.getActiveForegroundOperationId()).toBeNull();
  });

  it('does not let stale auth cancel the current foreground operation', () => {
    const controller = controllerAt(5);
    const cancel = vi.fn();
    controller.setForegroundMediaBinding({ mediaId: 'video', startNudge: () => cancel });
    controller.setForegroundTarget({ kind: 'live', serviceRef: 'current' });
    controller.reportForegroundVisibility(false, false);
    controller.reportForegroundVisibility(true, false);
    const operation = controller.getActiveForegroundOperationId();
    expect(operation).not.toBeNull();
    controller.dispatch({
      type: 'normative.playback.failure.raised', epoch: 2,
      failure: buildPlaybackFailure(
        { title: 'Stale forbidden', code: 'SESSION_FORBIDDEN', status: 403, retryable: false },
        'media-element', { class: 'auth', terminal: true },
      ),
    });
    expect(controller.getState().epoch.playback).toBe(5);
    expect(cancel).not.toHaveBeenCalled();
    expect(controller.getActiveForegroundOperationId()).toBe(operation);
  });

  it('does not start DOM work after a synchronous stop from the buffering callback', () => {
    const controller = controllerAt();
    const video = document.createElement('video');
    controller.setForegroundMediaBinding({
      mediaId: 'video',
      startNudge: (callbacks) => startResumePlaybackRecovery(video, callbacks),
      onTransitionToBuffering: () => { void controller.stop('user_stop'); },
    });
    controller.setForegroundTarget({ kind: 'live', serviceRef: 'channel-a' });
    controller.reportForegroundVisibility(false, false);
    controller.reportForegroundVisibility(true, false);
    expect(controller.getActiveForegroundOperationId()).toBeNull();
    expect(HTMLMediaElement.prototype.play).not.toHaveBeenCalled();
  });
});
