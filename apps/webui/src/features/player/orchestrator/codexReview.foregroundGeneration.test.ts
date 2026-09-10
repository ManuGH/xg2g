// Independent review fixture: copy beside playbackController.ts to run.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { createRecoveryLadderState } from './recoveryLadder';
import { buildPlaybackFailure } from './playbackMachine';
import type { LiveSessionTransport } from './liveSessionTransport';
import type { PlaybackDomainState } from './playbackTypes';

function createInitialState(): PlaybackDomainState {
  return {
    epoch: { playback: 1, session: 0 },
    traceId: '-',
    status: 'idle',
    playbackMode: 'UNKNOWN',
    vodStreamMode: null,
    activeHlsEngine: null,
    durationSeconds: null,
    canSeek: false,
    startUnix: null,
    sessionPhase: 'idle',
    mediaPhase: 'idle',
    contract: null,
    failure: null,
    lastAdvisory: null,
    explicitProfilePinned: false,
    hasSessionIntent: false,
    recovery: createRecoveryLadderState(),
    leaseExpiresAt: null,
    connectionLost: false,
  };
}

function createMockTransport(): LiveSessionTransport {
  return {
    fetchStreamInfo: vi.fn().mockResolvedValue({
      status: 200,
      data: { mode: 'direct_stream', playbackDecisionToken: 'tok-1' },
      headers: new Headers(),
    }),
    postStartIntent: vi.fn().mockResolvedValue({
      status: 200,
      data: { sessionId: 's-mock-1' },
      headers: new Headers(),
    }),
    waitForReady: vi.fn().mockResolvedValue({
      sessionId: 's-mock-1',
      playbackUrl: 'http://example.test/stream.m3u8',
      heartbeatIntervalSeconds: 5,
      leaseExpiresAt: '2026-09-09T12:00:00Z',
    }),
    postStopIntent: vi.fn().mockResolvedValue(undefined),
    postHeartbeat: vi.fn().mockResolvedValue({
      status: 200,
      data: { acknowledged: true, sessionId: 's-mock-1', leaseExpiresAt: '2026-09-09T12:05:00Z' },
      headers: new Headers(),
    }),
    fetchSessionSnapshot: vi.fn().mockResolvedValue({
      status: 200,
      data: { sessionId: 's-mock-1', state: 'READY' },
      headers: new Headers(),
    }),
  };
}



const controllers: PlaybackController[] = [];
afterEach(() => {
  controllers.splice(0).forEach((controller) => controller.dispose());
  vi.restoreAllMocks();
});

describe('Step 3b lifecycle generation and stale auth regressions', () => {
  it('rejects pre-disposal preparation after a healthy dispose/activate cycle', async () => {
    const transport = createMockTransport();
    const controller = createPlaybackController({ transport, createInitialState });
    controllers.push(controller);
    const oldEpoch = controller.allocatePlaybackEpoch();
    controller.beginPlaybackAttempt(oldEpoch, 'LIVE', 'starting', true);
    let finishPreparation!: () => void;
    const preparation = new Promise<void>((resolve) => { finishPreparation = resolve; });
    const oldContinuation = preparation.then(() => controller.startLive({
      epoch: oldEpoch, serviceRef: 'old-lifecycle-source',
    }));

    controller.dispose();
    controller.activate();
    finishPreparation();
    const result = await oldContinuation;

    expect(transport.fetchStreamInfo).not.toHaveBeenCalled();
    expect(result).toEqual({ status: 'cancelled', reason: 'superseded' });
  });

  it('keeps the current heartbeat supervising when stale auth from an older epoch arrives', async () => {
    const transport = createMockTransport();
    const controller = createPlaybackController({ transport, createInitialState });
    controllers.push(controller);
    const result = await controller.startLive({ serviceRef: 'current-live-source' });
    expect(result.status).toBe('ready');
    const currentEpoch = controller.getEpoch();
    expect(controller.isHeartbeatSupervising()).toBe(true);
    const heartbeatSession = controller.getHeartbeatSessionId();

    controller.dispatch({
      type: 'normative.playback.failure.raised',
      epoch: currentEpoch - 1,
      failure: buildPlaybackFailure(
        { title: 'Old forbidden', code: 'SESSION_FORBIDDEN', status: 403, retryable: false },
        'orchestrator', { class: 'auth', terminal: true },
      ),
    });

    expect(controller.getEpoch()).toBe(currentEpoch);
    expect(controller.isStalePlaybackEpoch(currentEpoch)).toBe(false);
    expect(controller.isHeartbeatSupervising()).toBe(true);
    expect(controller.getHeartbeatSessionId()).toBe(heartbeatSession);
  });
});
