// Independent review fixture: copy beside playbackController.ts to run.
import React, { useLayoutEffect } from 'react';
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { usePlaybackController } from './usePlaybackController';
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


function reportAuth(controller: PlaybackController) {
  controller.dispatch({
    type: 'normative.playback.failure.raised', epoch: controller.getEpoch(),
    failure: buildPlaybackFailure(
      { title: 'Forbidden', code: 'SESSION_FORBIDDEN', status: 403, retryable: false },
      'orchestrator', { class: 'auth', terminal: true },
    ),
  });
}
const controllers: PlaybackController[] = [];
afterEach(() => {
  cleanup();
  controllers.splice(0).forEach((c) => c.dispose());
  vi.restoreAllMocks();
});
describe('Activation must preserve terminal epoch fencing', () => {
  it('does not revive a fenced epoch when activate is called on an active controller', async () => {
    const transport = createMockTransport();
    const controller = createPlaybackController({ transport, createInitialState });
    controllers.push(controller);
    reportAuth(controller);
    const fencedEpoch = controller.getEpoch();
    expect(controller.isStalePlaybackEpoch(fencedEpoch)).toBe(true);
    controller.activate();
    const result = await controller.startLive({ epoch: fencedEpoch, serviceRef: 'obsolete-preparation' });
    expect(transport.fetchStreamInfo).not.toHaveBeenCalled();
    expect(result).toEqual({ status: 'cancelled', reason: 'superseded' });
  });

  it('preserves auth raised by a child layout effect before parent passive activation', async () => {
    const transport = createMockTransport();
    let exposed!: PlaybackController;
    let fencedInLayout = false;
    function Child({ controller }: { controller: PlaybackController }) {
      useLayoutEffect(() => {
        reportAuth(controller);
        fencedInLayout = controller.isStalePlaybackEpoch(controller.getEpoch());
      }, [controller]);
      return null;
    }
    function Parent() {
      const { controller } = usePlaybackController(transport, createInitialState, () => {});
      exposed = controller;
      return <Child controller={controller} />;
    }
    render(<Parent />);
    expect(fencedInLayout).toBe(true);
    expect(exposed.getState().status).toBe('error');
    let result: Awaited<ReturnType<PlaybackController['startLive']>> | undefined;
    await act(async () => {
      result = await exposed.startLive({ epoch: exposed.getEpoch(), serviceRef: 'obsolete-preparation' });
    });
    expect(transport.fetchStreamInfo).not.toHaveBeenCalled();
    expect(result).toEqual({ status: 'cancelled', reason: 'superseded' });
  });

  it('repeated activate calls are idempotent and preserve terminal auth fencing', async () => {
    const transport = createMockTransport();
    const controller = createPlaybackController({ transport, createInitialState });
    controllers.push(controller);
    reportAuth(controller);
    const fencedEpoch = controller.getEpoch();
    controller.activate();
    controller.activate();
    const result = await controller.startLive({ epoch: fencedEpoch, serviceRef: 'obsolete-preparation' });
    expect(transport.fetchStreamInfo).not.toHaveBeenCalled();
    expect(result).toEqual({ status: 'cancelled', reason: 'superseded' });
  });

  it('preserves explicit stop fence across dispose and reactivation', async () => {
    const transport = createMockTransport();
    const controller = createPlaybackController({ transport, createInitialState });
    controllers.push(controller);
    await controller.stop();
    const stoppedEpoch = controller.getEpoch();
    expect(controller.isStalePlaybackEpoch(stoppedEpoch)).toBe(true);
    controller.dispose();
    controller.activate();
    const result = await controller.startLive({ epoch: stoppedEpoch, serviceRef: 'obsolete-preparation' });
    expect(transport.fetchStreamInfo).not.toHaveBeenCalled();
    expect(result).toEqual({ status: 'cancelled', reason: 'superseded' });
  });

  it('preserves terminal auth across dispose and reactivation', async () => {
    const transport = createMockTransport();
    const controller = createPlaybackController({ transport, createInitialState });
    controllers.push(controller);
    reportAuth(controller);
    const fencedEpoch = controller.getEpoch();
    expect(controller.isStalePlaybackEpoch(fencedEpoch)).toBe(true);
    controller.dispose();
    controller.activate();
    const result = await controller.startLive({ epoch: fencedEpoch, serviceRef: 'obsolete-preparation' });
    expect(transport.fetchStreamInfo).not.toHaveBeenCalled();
    expect(result).toEqual({ status: 'cancelled', reason: 'superseded' });
  });

  it('permits a fresh explicit user start with a newly allocated epoch after terminal auth and reactivation', async () => {
    const transport = createMockTransport();
    const controller = createPlaybackController({ transport, createInitialState });
    controllers.push(controller);
    reportAuth(controller);
    const fencedEpoch = controller.getEpoch();
    expect(controller.isStalePlaybackEpoch(fencedEpoch)).toBe(true);
    controller.dispose();
    controller.activate();

    // Old epoch remains fenced
    expect(controller.isStalePlaybackEpoch(fencedEpoch)).toBe(true);

    // Fresh explicit start allocates a new epoch and proceeds cleanly
    let result: Awaited<ReturnType<PlaybackController['startLive']>> | undefined;
    await act(async () => {
      result = await controller.startLive({ serviceRef: 'fresh-service-stream' });
    });
    expect(transport.fetchStreamInfo).toHaveBeenCalledTimes(1);
    expect(transport.fetchStreamInfo).toHaveBeenCalledWith(
      expect.objectContaining({ serviceRef: 'fresh-service-stream' }),
    );
    expect(result?.status).toBe('ready');
  });

  it('restores lifecycle-disposed epoch on healthy reactivation under Root React.StrictMode', async () => {
    const transport = createMockTransport();
    let exposed!: PlaybackController;
    function Player() {
      const { controller } = usePlaybackController(transport, createInitialState, () => {});
      exposed = controller;
      return null;
    }
    render(
      <React.StrictMode>
        <Player />
      </React.StrictMode>,
    );
    expect(exposed.getState().status).not.toBe('error');
    expect(exposed.getState().status).not.toBe('stopped');
    const healthyEpoch = exposed.getEpoch();
    expect(exposed.isStalePlaybackEpoch(healthyEpoch)).toBe(false);

    let result: Awaited<ReturnType<PlaybackController['startLive']>> | undefined;
    await act(async () => {
      result = await exposed.startLive({ epoch: healthyEpoch, serviceRef: 'healthy-stream' });
    });
    expect(transport.fetchStreamInfo).toHaveBeenCalledTimes(1);
    expect(result?.status).toBe('ready');
  });

  it('preserves terminal auth raised during reactivation setup before passive activate', async () => {
    const transport = createMockTransport();
    let exposed!: PlaybackController;
    let authRaisedInLayout = false;
    function Child({ controller }: { controller: PlaybackController }) {
      useLayoutEffect(() => {
        reportAuth(controller);
        authRaisedInLayout = true;
      }, [controller]);
      return null;
    }
    function Parent() {
      const { controller } = usePlaybackController(transport, createInitialState, () => {});
      exposed = controller;
      return (
        <React.StrictMode>
          <Child controller={controller} />
        </React.StrictMode>
      );
    }
    render(<Parent />);
    expect(authRaisedInLayout).toBe(true);
    expect(exposed.getState().status).toBe('error');

    let result: Awaited<ReturnType<PlaybackController['startLive']>> | undefined;
    await act(async () => {
      result = await exposed.startLive({ epoch: exposed.getEpoch(), serviceRef: 'obsolete-preparation' });
    });
    expect(transport.fetchStreamInfo).not.toHaveBeenCalled();
    expect(result).toEqual({ status: 'cancelled', reason: 'superseded' });
  });
});
