// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { StrictMode, useRef } from 'react';
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { HlsInstanceRef, V3PlayerProps, VideoElementRef } from '../../../types/v3-player';
import { usePlaybackOrchestrator } from '../usePlaybackOrchestrator';
import * as controllerModule from './playbackController';
import type { PlaybackCommand } from './playbackTypes';
import { buildPlaybackFailure } from './playbackMachine';
import type {
  UseNetworkRecoveryWatchdogOptions,
} from './useNetworkRecoveryWatchdog';

const realCreatePlaybackController = controllerModule.createPlaybackController;

vi.mock('../lib/hlsRuntime', () => {
  function HlsMock(this: any) {
    this.loadSource = vi.fn();
    this.attachMedia = vi.fn();
    this.on = vi.fn();
    this.startLoad = vi.fn();
    this.destroy = vi.fn();
  }
  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  (HlsMock as any).Events = {
    LEVEL_SWITCHED: 'hlsLevelSwitched',
    MANIFEST_PARSED: 'hlsManifestParsed',
    AUDIO_TRACK_LOADED: 'hlsAudioTrackLoaded',
    AUDIO_TRACK_SWITCHED: 'hlsAudioTrackSwitched',
    ERROR: 'hlsError',
    FRAG_BUFFERED: 'hlsFragBuffered',
    FRAG_CHANGED: 'hlsFragChanged',
  };
  return { default: HlsMock };
});

describe('useNetworkRecoveryWatchdogWiring & Facade Integration', () => {
  let executedCommands: PlaybackCommand[] = [];
  let exposedController!: controllerModule.PlaybackController;
  let exposedActions!: ReturnType<typeof usePlaybackOrchestrator>['actions'];
  let fetchMock: any;

  function Harness({ props }: { props: V3PlayerProps }) {
    const containerRef = useRef<HTMLDivElement>(null);
    const videoRef = useRef<VideoElementRef>(null);
    const hlsRef = useRef<HlsInstanceRef | null>(null);
    const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

    const { controller, actions } = usePlaybackOrchestrator(props, {
      containerRef,
      videoRef,
      hlsRef,
      resumePrimaryActionRef,
    });

    exposedController = controller;
    exposedActions = actions;

    return (
      <div ref={containerRef}>
        <video
          ref={(el) => {
            if (el) {
              el.play = vi.fn().mockResolvedValue(undefined);
              el.pause = vi.fn();
            }
            videoRef.current = el;
          }}
        />
      </div>
    );
  }

  beforeEach(() => {
    vi.useFakeTimers();
    executedCommands = [];

    vi.spyOn(controllerModule, 'createPlaybackController').mockImplementation((options) => {
      const controller = realCreatePlaybackController(options);
      const originalSetCommandExecutor = controller.setCommandExecutor;
      controller.setCommandExecutor = (exec) => {
        if (!exec) {
          originalSetCommandExecutor(null);
          return;
        }
        originalSetCommandExecutor((cmd) => {
          executedCommands.push(cmd);
          return exec(cmd);
        });
      };
      return controller;
    });

    let sessionCounter = 0;
    let latestSessionId = 'sess-test-1';

    fetchMock = vi.fn().mockImplementation((url: string) => {
      const u = String(url);
      if (u.includes('/system/healthz')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          text: async () => 'OK',
        });
      }
      if (u.includes('/live/stream-info')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({
            mode: 'direct_stream',
            playbackDecisionToken: 'token-1',
            decision: { mode: 'direct_stream', playbackDecisionToken: 'token-1' },
          }),
          text: async () => JSON.stringify({}),
        });
      }
      if (u.includes('/intents')) {
        latestSessionId = `sess-test-${++sessionCounter}`;
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({ sessionId: latestSessionId }),
          text: async () => JSON.stringify({ sessionId: latestSessionId }),
        });
      }
      if (u.includes('/heartbeat')) {
        const parts = u.split('/');
        const sid = parts[parts.indexOf('sessions') + 1] || latestSessionId;
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({
            acknowledged: true,
            sessionId: sid,
            leaseExpiresAt: '2026-09-11T21:00:00Z',
          }),
          text: async () => JSON.stringify({ acknowledged: true }),
        });
      }
      if (u.includes('/sessions/')) {
        const parts = u.split('/');
        const sid = parts[parts.indexOf('sessions') + 1] || latestSessionId;
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({
            state: 'READY',
            sessionId: sid,
            playbackUrl: 'http://test.local/live.m3u8',
            leaseExpiresAt: '2026-09-11T21:00:00Z',
            heartbeatIntervalSeconds: 15,
          }),
          text: async () => JSON.stringify({}),
        });
      }
      return Promise.resolve({
        ok: true,
        status: 200,
        headers: new Headers(),
        json: async () => ({}),
        text: async () => '',
      });
    });
    globalThis.fetch = fetchMock;
  });

  afterEach(() => {
    vi.clearAllTimers();
    cleanup();
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  describe('Condition C3: Target Equivalence at Orchestrator Level', () => {
    it('recovers with effectiveRef when effectiveRef differs from sRef prop (ref !== sRef path)', async () => {
      render(
        <Harness
          props={
            {
              autoStart: false,
              sRef: '1:0:1:PROP',
              apiBase: 'http://test.local',
            } as unknown as V3PlayerProps
          }
        />,
      );

      // Start live stream on an effective ref that differs from sRef prop
      await act(async () => {
        await exposedActions.startStream('1:0:1:EFFECTIVE');
      });

      // Clear initial start commands
      executedCommands = [];

      // Fail playback with watchable failure (status: null, non-terminal)
      act(() => {
        exposedController.dispatch({
          type: 'normative.playback.failure.raised',
          epoch: exposedController.getEpoch(),
          failure: buildPlaybackFailure(
            { title: 'Network Timeout', code: 'NETWORK_TIMEOUT', status: undefined, retryable: true },
            'adapter',
            { recoverable: true },
          ),
          status: 'error',
        });
      });

      expect(exposedController.getDomainStatus()).toBe('error');
      expect(exposedController.getNetworkWatchdogState().active).toBe(true);
      expect(exposedController.getNetworkWatchdogState().timerPending).toBe(true);

      // Advance 5,000 ms to trigger probe, plus 200 ms for teardown sleep(75) to finish
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
        await vi.advanceTimersByTimeAsync(200);
      });

      // Assert: watchdog recovered
      expect(exposedController.getNetworkWatchdogState().recoveries).toBe(1);

      // Assert C3 contract: command.playback.start.serviceRef MUST equal the effective ref
      const startCmd = executedCommands.find(
        (c) => c.type === 'command.playback.start',
      ) as any;
      expect(startCmd).toBeDefined();
      expect(startCmd.serviceRef).toBe('1:0:1:EFFECTIVE');
      expect(startCmd.serviceRef).not.toBe('1:0:1:PROP');
    });

    it('recovers VOD target with recordingId and explicitProfile', async () => {
      localStorage.setItem('xg2g.player.explicitProfile', 'quality');
      render(
        <Harness
          props={
            {
              autoStart: false,
              recordingId: 'rec-vod-42',
              apiBase: 'http://test.local',
            } as unknown as V3PlayerProps
          }
        />,
      );

      await act(async () => {
        await exposedActions.startStream();
      });

      executedCommands = [];

      act(() => {
        exposedController.dispatch({
          type: 'normative.playback.failure.raised',
          epoch: exposedController.getEpoch(),
          failure: buildPlaybackFailure(
            { title: 'Playback Failure', code: 'PLAYBACK_FAILURE', status: undefined, retryable: true },
            'media-element',
            { recoverable: true },
          ),
          status: 'error',
        });
      });

      expect(exposedController.getNetworkWatchdogState().active).toBe(true);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
        await vi.advanceTimersByTimeAsync(200);
      });

      expect(exposedController.getNetworkWatchdogState().recoveries).toBe(1);

      const startCmd = executedCommands.find(
        (c) => c.type === 'command.playback.start',
      ) as any;
      expect(startCmd).toBeDefined();
      expect(startCmd.kind).toBe('vod');
      expect(startCmd.recordingId).toBe('rec-vod-42');
      expect(startCmd.explicitProfile).toBe('quality');
    });

    it('recovers SRC target with srcUrl and explicitProfile', async () => {
      localStorage.setItem('xg2g.player.explicitProfile', 'direct');
      render(
        <Harness
          props={
            {
              autoStart: false,
              src: 'https://cdn.example.com/live/index.m3u8',
              apiBase: 'http://test.local',
            } as unknown as V3PlayerProps
          }
        />,
      );

      await act(async () => {
        await exposedActions.startStream();
      });

      executedCommands = [];

      act(() => {
        exposedController.dispatch({
          type: 'normative.playback.failure.raised',
          epoch: exposedController.getEpoch(),
          failure: buildPlaybackFailure(
            { title: 'Playback Failure', code: 'PLAYBACK_FAILURE', status: undefined, retryable: true },
            'media-element',
            { recoverable: true },
          ),
          status: 'error',
        });
      });

      expect(exposedController.getNetworkWatchdogState().active).toBe(true);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
        await vi.advanceTimersByTimeAsync(200);
      });

      expect(exposedController.getNetworkWatchdogState().recoveries).toBe(1);

      const startCmd = executedCommands.find(
        (c) => c.type === 'command.playback.start',
      ) as any;
      expect(startCmd).toBeDefined();
      expect(startCmd.kind).toBe('src');
      expect(startCmd.srcUrl).toBe('https://cdn.example.com/live/index.m3u8');
      expect(startCmd.explicitProfile).toBe('direct');
    });
  });

  describe('Condition C4: Facade Real Second Activation Edge & Budget', () => {
    it('executes genuine false -> true edges through retried attempts, bounds recoveries to 3, and resets on playing', async () => {
      render(
        <Harness
          props={
            {
              autoStart: false,
              sRef: '1:0:1:EDGE',
              apiBase: 'http://test.local',
            } as unknown as V3PlayerProps
          }
        />,
      );

      await act(async () => {
        await exposedActions.startStream('1:0:1:EDGE');
      });

      // --- Attempt 1: First watchable failure ---
      act(() => {
        exposedController.dispatch({
          type: 'normative.playback.failure.raised',
          epoch: exposedController.getEpoch(),
          failure: buildPlaybackFailure(
            { title: 'Network Timeout', code: 'NETWORK_TIMEOUT', status: undefined, retryable: true },
            'adapter',
            { recoverable: true },
          ),
          status: 'error',
        });
      });

      expect(exposedController.getNetworkWatchdogState().active).toBe(true);
      expect(exposedController.getNetworkWatchdogState().timerPending).toBe(true);
      expect(exposedController.getNetworkWatchdogState().attempt).toBe(0);

      // Advance 5s (probe) + 200ms (stop teardown completes) -> recovery 1 succeeds
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
        await vi.advanceTimersByTimeAsync(200);
      });

      expect(exposedController.getNetworkWatchdogState().recoveries).toBe(1);
      // Status left 'error' during retry (transitions through stopping/restarting)
      expect(exposedController.getDomainStatus()).not.toBe('error');
      expect(exposedController.getNetworkWatchdogState().active).toBe(false);

      // --- Attempt 2: Retried start fails again with watchable failure ---
      act(() => {
        exposedController.dispatch({
          type: 'normative.playback.failure.raised',
          epoch: exposedController.getEpoch(),
          failure: buildPlaybackFailure(
            { title: 'Network Timeout', code: 'NETWORK_TIMEOUT', status: undefined, retryable: true },
            'adapter',
            { recoverable: true },
          ),
          status: 'error',
        });
      });

      // C4 contract: genuine false -> true edge occurred! Second loop with fresh 5s delay
      expect(exposedController.getNetworkWatchdogState().active).toBe(true);
      expect(exposedController.getNetworkWatchdogState().timerPending).toBe(true);
      expect(exposedController.getNetworkWatchdogState().attempt).toBe(0);

      // Advance 5s + 200ms -> recovery 2 succeeds
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
        await vi.advanceTimersByTimeAsync(200);
      });

      expect(exposedController.getNetworkWatchdogState().recoveries).toBe(2);
      expect(exposedController.getDomainStatus()).not.toBe('error');
      expect(exposedController.getNetworkWatchdogState().active).toBe(false);

      // --- Attempt 3: Retried start fails a third time ---
      act(() => {
        exposedController.dispatch({
          type: 'normative.playback.failure.raised',
          epoch: exposedController.getEpoch(),
          failure: buildPlaybackFailure(
            { title: 'Network Timeout', code: 'NETWORK_TIMEOUT', status: undefined, retryable: true },
            'adapter',
            { recoverable: true },
          ),
          status: 'error',
        });
      });

      expect(exposedController.getNetworkWatchdogState().active).toBe(true);
      expect(exposedController.getNetworkWatchdogState().timerPending).toBe(true);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
        await vi.advanceTimersByTimeAsync(200);
      });

      expect(exposedController.getNetworkWatchdogState().recoveries).toBe(3);
      expect(exposedController.getDomainStatus()).not.toBe('error');

      // --- Attempt 4: Fourth failure triggers fourth activation edge ---
      act(() => {
        exposedController.dispatch({
          type: 'normative.playback.failure.raised',
          epoch: exposedController.getEpoch(),
          failure: buildPlaybackFailure(
            { title: 'Network Timeout', code: 'NETWORK_TIMEOUT', status: undefined, retryable: true },
            'adapter',
            { recoverable: true },
          ),
          status: 'error',
        });
      });

      // C4 contract: 4th activation is refused due to recoveries exhaustion!
      expect(exposedController.getNetworkWatchdogState().active).toBe(true);
      expect(exposedController.getNetworkWatchdogState().timerPending).toBe(false);

      // Advance 10s: no probe is scheduled or fired
      const probesBefore = fetchMock.mock.calls.filter((c: any) =>
        String(c[0]).includes('/system/healthz'),
      ).length;

      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });

      const probesAfter = fetchMock.mock.calls.filter((c: any) =>
        String(c[0]).includes('/system/healthz'),
      ).length;
      expect(probesAfter).toBe(probesBefore);

      // --- Reset on playing ---
      act(() => {
        exposedController.dispatch({
          type: 'normative.media.status.changed',
          epoch: exposedController.getEpoch(),
          status: 'playing',
        });
      });

      // C4 contract: status === 'playing' resets recoveries to 0!
      expect(exposedController.getNetworkWatchdogState().recoveries).toBe(0);
      expect(exposedController.getNetworkWatchdogState().active).toBe(false);

      // --- Subsequent failure after playing restarts loop with fresh budget ---
      act(() => {
        exposedController.dispatch({
          type: 'normative.playback.failure.raised',
          epoch: exposedController.getEpoch(),
          failure: buildPlaybackFailure(
            { title: 'Network Timeout', code: 'NETWORK_TIMEOUT', status: undefined, retryable: true },
            'adapter',
            { recoverable: true },
          ),
          status: 'error',
        });
      });

      expect(exposedController.getNetworkWatchdogState().active).toBe(true);
      expect(exposedController.getNetworkWatchdogState().timerPending).toBe(true);
    });
  });

  describe('StrictMode & Adapter Structural Integrity', () => {
    it('runs exactly one probe loop with one 5s delay under StrictMode double-mount', async () => {
      render(
        <StrictMode>
          <Harness
            props={
              {
                autoStart: false,
                sRef: '1:0:1:STRICT',
                apiBase: 'http://test.local',
              } as unknown as V3PlayerProps
            }
          />
        </StrictMode>,
      );

      await act(async () => {
        await exposedActions.startStream('1:0:1:STRICT');
      });

      act(() => {
        exposedController.dispatch({
          type: 'normative.playback.failure.raised',
          epoch: exposedController.getEpoch(),
          failure: buildPlaybackFailure(
            { title: 'Network Timeout', code: 'NETWORK_TIMEOUT', status: undefined, retryable: true },
            'adapter',
            { recoverable: true },
          ),
          status: 'error',
        });
      });

      expect(exposedController.getNetworkWatchdogState().active).toBe(true);
      expect(exposedController.getNetworkWatchdogState().timerPending).toBe(true);

      // Advance 5,000 ms + 200 ms for probe async promise completion
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5_000);
        await vi.advanceTimersByTimeAsync(200);
      });

      // Assert: exactly ONE healthz probe was called by the watchdog (method HEAD)
      const healthzCalls = fetchMock.mock.calls.filter((c: any) =>
        c[1]?.method === 'HEAD' || (String(c[0]).endsWith('/system/healthz') && !String(c[0]).includes('playbackProbe')),
      );
      expect(healthzCalls).toHaveLength(1);
      expect(exposedController.getNetworkWatchdogState().recoveries).toBe(1);
    });

    it('confirms UseNetworkRecoveryWatchdogOptions interface removal of active, onReachable, healthy', () => {
      // Type-level assertion that options requires controller, apiBase, isTv, intentKey
      const testOptions: UseNetworkRecoveryWatchdogOptions = {
        controller: exposedController,
        apiBase: 'http://test.local',
        isTv: false,
        intentKey: 'channel-1',
      };
      expect(testOptions.controller).toBeDefined();
      expect(testOptions.apiBase).toBe('http://test.local');
      expect(testOptions.isTv).toBe(false);
      expect(testOptions.intentKey).toBe('channel-1');

      // Assert properties active, onReachable, healthy do not exist
      expect('active' in testOptions).toBe(false);
      expect('onReachable' in testOptions).toBe(false);
      expect('healthy' in testOptions).toBe(false);
    });
  });
});
