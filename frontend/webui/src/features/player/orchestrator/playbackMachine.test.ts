import { describe, expect, it } from 'vitest';
import { buildPlaybackFailure, createInitialPlaybackDomainState, playbackMachine, runPlaybackMachine } from './playbackMachine';

describe('playbackMachine', () => {
  it('resets normative domain state when a new playback attempt starts', () => {
    const initial = createInitialPlaybackDomainState(900);
    const state = playbackMachine(initial, {
      type: 'normative.playback.attempt.started',
      epoch: 3,
      playbackMode: 'VOD',
      status: 'building',
      requestedDuration: 1200,
    });

    expect(state.epoch.playback).toBe(3);
    expect(state.status).toBe('building');
    expect(state.playbackMode).toBe('VOD');
    expect(state.durationSeconds).toBe(1200);
    expect(state.sessionPhase).toBe('idle');
    expect(state.contract).toBeNull();
    expect(state.failure).toBeNull();
  });

  it('ignores stale contract resolutions from an older playback epoch', () => {
    const started = playbackMachine(createInitialPlaybackDomainState(), {
      type: 'normative.playback.attempt.started',
      epoch: 4,
      playbackMode: 'VOD',
      status: 'building',
      requestedDuration: null,
    });
    const superseded = playbackMachine(started, {
      type: 'normative.playback.attempt.started',
      epoch: 5,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });
    const staleResolution = playbackMachine(superseded, {
      type: 'normative.playback.contract.resolved',
      epoch: 4,
      contract: {
        kind: 'recording',
        requestId: 'req-stale',
        mode: 'direct_mp4',
        streamUrl: 'https://stale.example/video.mp4',
        canSeek: true,
        live: false,
        autoplayAllowed: true,
        sessionRequired: false,
        sessionId: null,
        expiresAt: null,
        decisionToken: null,
        durationSeconds: 1800,
        startUnix: 123,
        mimeType: 'video/mp4',
      },
    });

    expect(staleResolution.epoch.playback).toBe(5);
    expect(staleResolution.playbackMode).toBe('LIVE');
    expect(staleResolution.contract).toBeNull();
    expect(staleResolution.traceId).toBe('-');
  });

  it('ignores stale session phase updates from an older session epoch', () => {
    const started = playbackMachine(createInitialPlaybackDomainState(), {
      type: 'normative.playback.attempt.started',
      epoch: 7,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });
    const sessionOne = playbackMachine(started, {
      type: 'normative.session.attempt.started',
      playbackEpoch: 7,
      sessionEpoch: 1,
    });
    const sessionTwo = playbackMachine(sessionOne, {
      type: 'normative.session.attempt.started',
      playbackEpoch: 7,
      sessionEpoch: 2,
    });
    const stalePhase = playbackMachine(sessionTwo, {
      type: 'normative.session.phase.changed',
      playbackEpoch: 7,
      sessionEpoch: 1,
      phase: 'ready',
      requestId: 'req-old-session',
    });

    expect(stalePhase.epoch.session).toBe(2);
    expect(stalePhase.sessionPhase).toBe('starting');
    expect(stalePhase.traceId).toBe('-');
  });

  it('builds structured failures instead of collapsing to plain strings', () => {
    const failure = buildPlaybackFailure({
      title: 'Authentication required',
      status: 401,
      retryable: false,
    }, 'backend');

    expect(failure.class).toBe('auth');
    expect(failure.retryable).toBe(false);
    expect(failure.terminal).toBe(true);
    expect(failure.source).toBe('backend');
    expect(failure.status).toBe(401);
    expect(failure.policyImpact).toBe('blocked');
  });

  it('carries profile and session-intent context into a new attempt', () => {
    const state = playbackMachine(createInitialPlaybackDomainState(), {
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
      explicitProfilePinned: true,
      hasSessionIntent: true,
    });

    expect(state.explicitProfilePinned).toBe(true);
    expect(state.hasSessionIntent).toBe(true);
    expect(state.recovery.autoFallbackUsed).toBe(false);
  });

  it('schedules one repair restart for an unpinned session media failure', () => {
    const started = playbackMachine(createInitialPlaybackDomainState(), {
      type: 'normative.playback.attempt.started',
      epoch: 2,
      playbackMode: 'LIVE',
      status: 'playing',
      requestedDuration: null,
      explicitProfilePinned: false,
      hasSessionIntent: true,
    });
    const failure = buildPlaybackFailure({
      title: 'Decoder exhausted',
      code: 'DECODE_EXHAUSTED',
      retryable: true,
    }, 'media-element', {
      recoverable: true,
    });

    const first = runPlaybackMachine(started, {
      type: 'normative.playback.failure.raised',
      epoch: 2,
      failure,
    });
    expect(first.state.status).toBe('recovering');
    expect(first.state.failure).toBeNull();
    expect(first.state.recovery.autoFallbackUsed).toBe(true);
    expect(first.commands).toEqual([{
      type: 'command.playback.schedule_auto_fallback',
      epoch: 2,
      delayMs: 250,
      profile: 'repair',
      failureCode: 'DECODE_EXHAUSTED',
      failureClass: 'media',
    }]);

    const repeated = runPlaybackMachine(first.state, {
      type: 'normative.playback.failure.raised',
      epoch: 2,
      failure,
    });
    expect(repeated.commands).toEqual([]);
    expect(repeated.state.status).toBe('error');
  });

  it('does not override pinned profiles or retry static sources with repair', () => {
    const failure = buildPlaybackFailure({
      title: 'Decoder exhausted',
      code: 'DECODE_EXHAUSTED',
      retryable: true,
    }, 'media-element', {
      recoverable: true,
    });
    const pinned = playbackMachine(createInitialPlaybackDomainState(), {
      type: 'normative.playback.attempt.started',
      epoch: 3,
      playbackMode: 'LIVE',
      status: 'playing',
      requestedDuration: null,
      explicitProfilePinned: true,
      hasSessionIntent: true,
    });
    const staticSource = playbackMachine(createInitialPlaybackDomainState(), {
      type: 'normative.playback.attempt.started',
      epoch: 4,
      playbackMode: 'VOD',
      status: 'playing',
      requestedDuration: null,
      explicitProfilePinned: false,
      hasSessionIntent: false,
    });

    expect(runPlaybackMachine(pinned, {
      type: 'normative.playback.failure.raised',
      epoch: 3,
      failure,
    }).commands).toEqual([]);
    expect(runPlaybackMachine(staticSource, {
      type: 'normative.playback.failure.raised',
      epoch: 4,
      failure,
    }).commands).toEqual([]);
  });
});
