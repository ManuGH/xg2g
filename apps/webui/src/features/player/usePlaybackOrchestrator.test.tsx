import { StrictMode, createRef, useRef, useState } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { HlsInstanceRef, V3PlayerProps, VideoElementRef } from '../../types/v3-player';
import { usePlaybackOrchestrator } from './usePlaybackOrchestrator';
import { buildPlaybackFailure } from './orchestrator/playbackMachine';
import * as networkProbeModule from './utils/playbackNetworkProbe';
import { client } from '../../client-ts/client.gen';

vi.mock('./lib/hlsRuntime', () => {
  const HlsMock = vi.fn();
  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  return { default: HlsMock };
});

function Harness({ props }: { props: V3PlayerProps }) {
  const containerRef = createRef<HTMLDivElement>();
  const videoRef = createRef<VideoElementRef>();
  const hlsRef = useRef<HlsInstanceRef>(null);
  const resumePrimaryActionRef = createRef<HTMLButtonElement>();
  const { viewState, actions } = usePlaybackOrchestrator(props, {
    containerRef,
    videoRef,
    hlsRef,
    resumePrimaryActionRef,
  });

  return (
    <div>
      <div data-testid="show-service-input">{String(viewState.showServiceInput)}</div>
      <div data-testid="show-manual-start">{String(viewState.showManualStartButton)}</div>
      <div data-testid="service-ref">{viewState.serviceRef}</div>
      <div data-testid="duration-seconds">{String(viewState.playback.durationSeconds)}</div>
      <button onClick={() => actions.updateServiceRef('1:0:1:AA')} type="button">
        update-service-ref
      </button>
    </div>
  );
}

describe('usePlaybackOrchestrator', () => {
  it('derives the manual-start view state when no explicit playback source exists', () => {
    render(<Harness props={{ autoStart: false } as unknown as V3PlayerProps} />);

    expect(screen.getByTestId('show-service-input')).toHaveTextContent('true');
    expect(screen.getByTestId('show-manual-start')).toHaveTextContent('true');
    expect(screen.getByTestId('duration-seconds')).toHaveTextContent('null');
  });

  it('updates the exposed service-ref view state through explicit controller actions', () => {
    render(<Harness props={{ autoStart: false } as unknown as V3PlayerProps} />);

    fireEvent.click(screen.getByRole('button', { name: 'update-service-ref' }));

    expect(screen.getByTestId('service-ref')).toHaveTextContent('1:0:1:AA');
  });

  it('suppresses manual-start affordances for explicit recording playback props', () => {
    render(<Harness props={{ autoStart: false, recordingId: 'rec-123' }} />);

    expect(screen.getByTestId('show-service-input')).toHaveTextContent('false');
    expect(screen.getByTestId('show-manual-start')).toHaveTextContent('false');
  });

  describe('Facade lifecycle, preparation invalidation & mode-switch', () => {
    let fetchMock: any;

    beforeEach(() => {
      vi.clearAllMocks();
      vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined as never);
      vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
    });

    afterEach(() => {
      vi.restoreAllMocks();
    });

    it('stops in-flight preparation before network probe resolves, preventing start intent and keeping state stopped', async () => {
      let probeResolve: (value: any) => void;
      const probePromise = new Promise((res) => { probeResolve = res; });
      vi.spyOn(networkProbeModule, 'measurePlaybackNetwork').mockReturnValue(probePromise as any);

      fetchMock = vi.fn().mockImplementation((url: string) => {
        const u = String(url);
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
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({ sessionId: 'sess-probe-1' }),
            text: async () => JSON.stringify({ sessionId: 'sess-probe-1' }),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({}),
          text: async () => JSON.stringify({}),
        });
      });
      vi.stubGlobal('fetch', fetchMock);

      function FacadeProbeHarness() {
        const containerRef = useRef<HTMLDivElement>(null);
        const videoRef = useRef<VideoElementRef>(null);
        const hlsRef = useRef<HlsInstanceRef>(null);
        const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

        const { actions, playbackState } = usePlaybackOrchestrator(
          { autoStart: false } as unknown as V3PlayerProps,
          { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
        );

        return (
          <div>
            <span data-testid="status">{playbackState.status}</span>
            <button onClick={() => void actions.startStream('1:0:1:AA')} type="button">
              start
            </button>
            <button onClick={() => void actions.stopStream()} type="button">
              stop
            </button>
          </div>
        );
      }

      render(<FacadeProbeHarness />);
      expect(screen.getByTestId('status')).toHaveTextContent('idle');

      // 1. Click startStream
      fireEvent.click(screen.getByRole('button', { name: 'start' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('starting');
      });

      // 2. Click stopStream while probe is still in flight
      fireEvent.click(screen.getByRole('button', { name: 'stop' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('stopped');
      });

      // 3. Resolve the deferred network probe now
      probeResolve!({ kind: 'direct', rttMs: 5 });
      await act(async () => {
        await Promise.resolve();
        await new Promise((r) => setTimeout(r, 50));
      });

      // 4. Assertions: state remains stopped, and no intent start was sent
      expect(screen.getByTestId('status')).toHaveTextContent('stopped');
      const intentCalls = fetchMock.mock.calls.filter((c: any[]) => String(c[0]).includes('/intents'));
      const startIntents = intentCalls.filter((c: any[]) => {
        const body = c[1]?.body ? JSON.parse(String(c[1].body)) : {};
        return body?.type === 'stream.start';
      });
      expect(startIntents).toHaveLength(0);
    });

    it('handles start -> stop -> start on the facade without stale epoch interference', async () => {
      vi.spyOn(networkProbeModule, 'measurePlaybackNetwork').mockResolvedValue({ kind: 'direct', rttMs: 5 } as any);

      let sessionCounter = 0;
      fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
        const u = String(url);
        if (u.includes('/live/stream-info')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              mode: 'direct_stream',
              playbackDecisionToken: 'token-live',
              decision: { mode: 'direct_stream', playbackDecisionToken: 'token-live' },
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/intents')) {
          const body = init?.body ? JSON.parse(String(init.body)) : {};
          if (body?.type === 'stream.start') {
            sessionCounter++;
            const sessId = `sess-facade-${sessionCounter}`;
            return Promise.resolve({
              ok: true,
              status: 200,
              headers: new Headers(),
              json: async () => ({ sessionId: sessId }),
              text: async () => JSON.stringify({ sessionId: sessId }),
            });
          }
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({}),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/sessions/sess-facade-')) {
          const sessMatch = u.match(/sess-facade-\d+/);
          const sessId = sessMatch ? sessMatch[0] : 'sess-facade-1';
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              sessionId: sessId,
              state: 'READY',
              mode: 'LIVE',
              playbackUrl: `http://test/${sessId}.m3u8`,
              heartbeatIntervalSeconds: 5,
              leaseExpiresAt: '2026-09-09T22:00:00Z',
            }),
            text: async () => JSON.stringify({}),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({}),
          text: async () => JSON.stringify({}),
        });
      });
      vi.stubGlobal('fetch', fetchMock);

      function FacadeRestartHarness() {
        const containerRef = useRef<HTMLDivElement>(null);
        const videoRef = useRef<VideoElementRef>(null);
        const hlsRef = useRef<HlsInstanceRef>(null);
        const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

        const { actions, playbackState } = usePlaybackOrchestrator(
          { autoStart: false } as unknown as V3PlayerProps,
          { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
        );

        return (
          <div>
            <span data-testid="status">{playbackState.status}</span>
            <button onClick={() => void actions.startStream('1:0:1:AA')} type="button">
              start-A
            </button>
            <button onClick={() => void actions.startStream('1:0:1:BB')} type="button">
              start-B
            </button>
            <button onClick={() => void actions.stopStream()} type="button">
              stop
            </button>
          </div>
        );
      }

      render(<FacadeRestartHarness />);

      // Start A -> ready
      fireEvent.click(screen.getByRole('button', { name: 'start-A' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('ready');
      });

      // Stop -> stopped
      fireEvent.click(screen.getByRole('button', { name: 'stop' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('stopped');
      });

      // Start B -> ready
      fireEvent.click(screen.getByRole('button', { name: 'start-B' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('ready');
      });
    });

    it('retires ready Live session when switching to VOD on the facade', async () => {
      vi.spyOn(networkProbeModule, 'measurePlaybackNetwork').mockResolvedValue({ kind: 'direct', rttMs: 5 } as any);

      fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
        const u = String(url);
        if (u.includes('/live/stream-info')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              mode: 'direct_stream',
              playbackDecisionToken: 'token-live',
              decision: { mode: 'direct_stream', playbackDecisionToken: 'token-live' },
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/intents')) {
          const body = init?.body ? JSON.parse(String(init.body)) : {};
          if (body?.type === 'stream.start') {
            return Promise.resolve({
              ok: true,
              status: 200,
              headers: new Headers(),
              json: async () => ({ sessionId: 'sess-live-to-retire' }),
              text: async () => JSON.stringify({ sessionId: 'sess-live-to-retire' }),
            });
          }
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({}),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/sessions/sess-live-to-retire')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              sessionId: 'sess-live-to-retire',
              state: 'READY',
              mode: 'LIVE',
              playbackUrl: 'http://test/live.m3u8',
              heartbeatIntervalSeconds: 5,
              leaseExpiresAt: '2026-09-09T22:00:00Z',
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/recordings/rec-123/playback-info')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              recordingId: 'rec-123',
              mode: 'direct',
              playbackUrl: 'http://test/vod.m3u8',
            }),
            text: async () => JSON.stringify({}),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({}),
          text: async () => JSON.stringify({}),
        });
      });
      vi.stubGlobal('fetch', fetchMock);

      function DynamicFacadeHarness() {
        const [props, setProps] = useState<V3PlayerProps>({ autoStart: false } as unknown as V3PlayerProps);
        const containerRef = useRef<HTMLDivElement>(null);
        const videoRef = useRef<VideoElementRef>(null);
        const hlsRef = useRef<HlsInstanceRef>(null);
        const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

        const { actions, playbackState } = usePlaybackOrchestrator(
          props,
          { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
        );

        return (
          <div>
            <span data-testid="status">{playbackState.status}</span>
            <span data-testid="mode">{playbackState.playbackMode}</span>
            <button onClick={() => void actions.startStream('1:0:1:AA')} type="button">
              start-live
            </button>
            <button
              onClick={() => {
                setProps({ autoStart: true, recordingId: 'rec-123' } as unknown as V3PlayerProps);
              }}
              type="button"
            >
              switch-vod
            </button>
          </div>
        );
      }

      render(<DynamicFacadeHarness />);

      // Start Live -> ready
      fireEvent.click(screen.getByRole('button', { name: 'start-live' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('ready');
        expect(screen.getByTestId('mode')).toHaveTextContent('LIVE');
      });

      // Switch to VOD
      fireEvent.click(screen.getByRole('button', { name: 'switch-vod' }));

      // Verify stop intent was sent for sess-live-to-retire
      await waitFor(() => {
        const intentCalls = fetchMock.mock.calls.filter((c: any[]) => String(c[0]).includes('/intents'));
        const stopCalls = intentCalls.filter((c: any[]) => {
          const body = c[1]?.body ? JSON.parse(String(c[1].body)) : {};
          return body?.type === 'stream.stop' && body?.sessionId === 'sess-live-to-retire';
        });
        expect(stopCalls.length).toBeGreaterThanOrEqual(1);
      });
    });

    it('supervises Live session heartbeat and reflects lease state through orchestrator facade', async () => {
      let heartbeatCallCount = 0;

      fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
        const u = String(url);
        if (u.includes('/live/stream-info')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              mode: 'direct_stream',
              playbackDecisionToken: 'tok-hb',
              decision: { mode: 'direct_stream', playbackDecisionToken: 'tok-hb' },
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/intents')) {
          const body = init?.body ? JSON.parse(String(init.body)) : {};
          if (body.type === 'stream.start') {
            return Promise.resolve({
              ok: true,
              status: 200,
              headers: new Headers(),
              json: async () => ({ sessionId: 'sess-hb-live' }),
              text: async () => JSON.stringify({ sessionId: 'sess-hb-live' }),
            });
          }
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({}),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/sessions/sess-hb-live/heartbeat')) {
          heartbeatCallCount++;
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              acknowledged: true,
              sessionId: 'sess-hb-live',
              leaseExpiresAt: `2026-09-09T22:05:${10 * heartbeatCallCount}Z`,
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/sessions/sess-hb-live')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              sessionId: 'sess-hb-live',
              state: 'READY',
              mode: 'LIVE',
              playbackUrl: 'http://test/live.m3u8',
              heartbeatIntervalSeconds: 1,
              leaseExpiresAt: '2026-09-09T22:00:00Z',
            }),
            text: async () => JSON.stringify({}),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({}),
          text: async () => JSON.stringify({}),
        });
      });
      vi.stubGlobal('fetch', fetchMock);

      function HeartbeatFacadeHarness() {
        const containerRef = useRef<HTMLDivElement>(null);
        const videoRef = useRef<VideoElementRef>(null);
        const hlsRef = useRef<HlsInstanceRef>(null);
        const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

        const { actions, playbackState } = usePlaybackOrchestrator(
          { autoStart: false } as unknown as V3PlayerProps,
          { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
        );

        return (
          <div>
            <span data-testid="status">{playbackState.status}</span>
            <span data-testid="mode">{playbackState.playbackMode}</span>
            <span data-testid="lease">{playbackState.leaseExpiresAt ?? 'none'}</span>
            <button onClick={() => void actions.startStream('1:0:1:BB')} type="button">
              start-live
            </button>
            <button onClick={() => void actions.stopStream(false)} type="button">
              stop-live
            </button>
          </div>
        );
      }

      render(<HeartbeatFacadeHarness />);

      // Start Live -> ready
      fireEvent.click(screen.getByRole('button', { name: 'start-live' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('ready');
        expect(screen.getByTestId('mode')).toHaveTextContent('LIVE');
        expect(screen.getByTestId('lease')).toHaveTextContent('2026-09-09T22:00:00Z');
      });

      // Wait for 1st heartbeat to extend lease
      await waitFor(() => {
        expect(heartbeatCallCount).toBeGreaterThanOrEqual(1);
        expect(screen.getByTestId('lease')).not.toHaveTextContent('2026-09-09T22:00:00Z');
      });

      // Stop stream
      fireEvent.click(screen.getByRole('button', { name: 'stop-live' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('stopped');
        expect(screen.getByTestId('lease')).toHaveTextContent('none');
      });

      // Stop intent was sent
      const intentCalls = fetchMock.mock.calls.filter((c: any[]) => String(c[0]).includes('/intents'));
      const stopCalls = intentCalls.filter((c: any[]) => {
        const body = c[1]?.body ? JSON.parse(String(c[1].body)) : {};
        return body?.type === 'stream.stop' && body?.sessionId === 'sess-hb-live';
      });
      expect(stopCalls.length).toBeGreaterThanOrEqual(1);
    });

    it('demonstrates machine failure -> facade target capture -> controller timer -> actual restart path', async () => {
      vi.useFakeTimers();
      try {
        vi.spyOn(networkProbeModule, 'measurePlaybackNetwork').mockReturnValue(Promise.resolve(null as any));

        let streamInfoCalls = 0;
        fetchMock = vi.fn().mockImplementation((url: string) => {
          const u = String(url);
          if (u.includes('/live/stream-info')) {
            streamInfoCalls += 1;
            return Promise.resolve({
              ok: true,
              status: 200,
              headers: new Headers(),
              json: async () => ({
                mode: 'direct_stream',
                playbackDecisionToken: `token-auto-fb-${streamInfoCalls}`,
                decision: { mode: 'direct_stream', playbackDecisionToken: `token-auto-fb-${streamInfoCalls}` },
              }),
              text: async () => JSON.stringify({}),
            });
          }
          if (u.includes('/intents')) {
            return Promise.resolve({
              ok: true,
              status: 200,
              headers: new Headers(),
              json: async () => ({ sessionId: `sess-auto-fb-${streamInfoCalls}` }),
              text: async () => JSON.stringify({ sessionId: `sess-auto-fb-${streamInfoCalls}` }),
            });
          }
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({}),
            text: async () => JSON.stringify({}),
          });
        });
        vi.stubGlobal('fetch', fetchMock);

        let exposedController!: any;
        let exposedActions!: any;

        function FallbackFacadeHarness() {
          const containerRef = useRef<HTMLDivElement>(null);
          const videoRef = useRef<VideoElementRef>(null);
          const hlsRef = useRef<HlsInstanceRef>(null);
          const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

          const { controller, playbackState, actions } = usePlaybackOrchestrator(
            { autoStart: false } as unknown as V3PlayerProps,
            { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
          );
          exposedController = controller;
          exposedActions = actions;

          return (
            <div>
              <span data-testid="status">{playbackState.status}</span>
              <button onClick={() => void actions.startStream('1:0:1:CC')} type="button">
                start-live
              </button>
            </div>
          );
        }

        render(<FallbackFacadeHarness />);
        expect(exposedController).toBeDefined();

        // Start initial stream
        await act(async () => {
          await exposedActions.startStream('1:0:1:CC');
        });
        expect(streamInfoCalls).toBe(1);
        const currentEpoch = exposedController.getEpoch();

        // Controller should not have scheduled fallback initially
        expect(exposedController.hasScheduledAutoFallback(currentEpoch)).toBe(false);

        // Machine failure occurs: dispatch normative.playback.failure.raised
        // State machine decides recovery ladder restart and generates command.playback.schedule_auto_fallback
        // Orchestrator executes command, captures facade target ('1:0:1:CC', 'repair'), and schedules via controller
        act(() => {
          exposedController.dispatch({
            type: 'normative.playback.failure.raised',
            epoch: currentEpoch,
            failure: buildPlaybackFailure(
              { title: 'Decoder exhausted', code: 'DECODE_EXHAUSTED', retryable: true },
              'media-element',
              { recoverable: true },
            ),
          });
        });

        // Controller now has the timer scheduled
        expect(exposedController.hasScheduledAutoFallback(currentEpoch)).toBe(true);
        expect(streamInfoCalls).toBe(1); // No restart yet before deadline

        // Advance timers by 249ms: timer has not fired yet
        await act(async () => {
          vi.advanceTimersByTime(249);
        });
        expect(streamInfoCalls).toBe(1);

        // Advance 1ms to reach 250ms deadline: controller fires, dispatches start request,
        // orchestrator receives command.playback.start and executes actual restart
        await act(async () => {
          await vi.advanceTimersByTimeAsync(1);
        });

        expect(streamInfoCalls).toBe(2);
        expect(exposedController.hasScheduledAutoFallback(currentEpoch)).toBe(false);
      } finally {
        vi.useRealTimers();
      }
    });

    it('cancels scheduled recovery fallback on stop and channel replacement', async () => {
      vi.useFakeTimers();
      try {
        vi.spyOn(networkProbeModule, 'measurePlaybackNetwork').mockReturnValue(Promise.resolve(null as any));

        let streamInfoCalls = 0;
        fetchMock = vi.fn().mockImplementation((url: string) => {
          const u = String(url);
          if (u.includes('/live/stream-info')) {
            streamInfoCalls += 1;
            return Promise.resolve({
              ok: true,
              status: 200,
              headers: new Headers(),
              json: async () => ({
                mode: 'direct_stream',
                playbackDecisionToken: `token-cancel-${streamInfoCalls}`,
                decision: { mode: 'direct_stream', playbackDecisionToken: `token-cancel-${streamInfoCalls}` },
              }),
              text: async () => JSON.stringify({}),
            });
          }
          if (u.includes('/intents')) {
            return Promise.resolve({
              ok: true,
              status: 200,
              headers: new Headers(),
              json: async () => ({ sessionId: `sess-cancel-${streamInfoCalls}` }),
              text: async () => JSON.stringify({ sessionId: `sess-cancel-${streamInfoCalls}` }),
            });
          }
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({}),
            text: async () => JSON.stringify({}),
          });
        });
        vi.stubGlobal('fetch', fetchMock);

        let exposedController!: any;
        let exposedActions!: any;

        function FallbackCancelHarness() {
          const containerRef = useRef<HTMLDivElement>(null);
          const videoRef = useRef<VideoElementRef>(null);
          const hlsRef = useRef<HlsInstanceRef>(null);
          const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

          const { controller, playbackState, actions } = usePlaybackOrchestrator(
            { autoStart: false } as unknown as V3PlayerProps,
            { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
          );
          exposedController = controller;
          exposedActions = actions;

          return (
            <div>
              <span data-testid="status">{playbackState.status}</span>
              <button onClick={() => void actions.startStream('1:0:1:CC')} type="button">
                start-live
              </button>
              <button onClick={() => void actions.stopStream(false)} type="button">
                stop-live
              </button>
            </div>
          );
        }

        render(<FallbackCancelHarness />);

        // --- Part A: Cancellation on stop ---
        await act(async () => {
          await exposedActions.startStream('1:0:1:CC');
        });
        expect(streamInfoCalls).toBe(1);
        const epoch1 = exposedController.getEpoch();

        // Trigger failure -> schedule fallback
        act(() => {
          exposedController.dispatch({
            type: 'normative.playback.failure.raised',
            epoch: epoch1,
            failure: buildPlaybackFailure(
              { title: 'Decoder exhausted', code: 'DECODE_EXHAUSTED', retryable: true },
              'media-element',
              { recoverable: true },
            ),
          });
        });
        expect(exposedController.hasScheduledAutoFallback(epoch1)).toBe(true);

        // Explicit user stop cancels the fallback
        await act(async () => {
          await exposedActions.stopStream(false);
        });
        expect(exposedController.hasScheduledAutoFallback(epoch1)).toBe(false);

        // Advance timers past deadline: no restart fires
        await act(async () => {
          vi.advanceTimersByTime(1000);
        });
        expect(streamInfoCalls).toBe(1);

        // --- Part B: Cancellation on channel change (source replacement) ---
        await act(async () => {
          await exposedActions.startStream('1:0:1:CC');
        });
        expect(streamInfoCalls).toBe(2);
        const epoch2 = exposedController.getEpoch();

        // Trigger failure for epoch 2 -> schedule fallback
        act(() => {
          exposedController.dispatch({
            type: 'normative.playback.failure.raised',
            epoch: epoch2,
            failure: buildPlaybackFailure(
              { title: 'Decoder exhausted', code: 'DECODE_EXHAUSTED', retryable: true },
              'media-element',
              { recoverable: true },
            ),
          });
        });
        expect(exposedController.hasScheduledAutoFallback(epoch2)).toBe(true);

        // Tune to channel D -> supersedes epoch 2 and cancels fallback
        await act(async () => {
          await exposedActions.startStream('1:0:1:DD');
        });
        expect(streamInfoCalls).toBe(3); // One start for channel D
        expect(exposedController.hasScheduledAutoFallback(epoch2)).toBe(false);

        // Advance timers: old channel fallback never fires
        await act(async () => {
          vi.advanceTimersByTime(1000);
        });
        expect(streamInfoCalls).toBe(3);
      } finally {
        vi.useRealTimers();
      }
    });

    it('executes controller-owned retry sequencing through actions.retry without calling onClose', async () => {
      let streamInfoCalls = 0;
      let onCloseCalled = false;

      fetchMock = vi.fn().mockImplementation((url: string) => {
        const u = String(url);
        if (u.includes('/live/stream-info')) {
          streamInfoCalls += 1;
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              mode: 'direct_stream',
              playbackDecisionToken: `token-retry-${streamInfoCalls}`,
              decision: { mode: 'direct_stream', playbackDecisionToken: `token-retry-${streamInfoCalls}` },
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/intents')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({ sessionId: `sess-retry-${streamInfoCalls}` }),
            text: async () => JSON.stringify({ sessionId: `sess-retry-${streamInfoCalls}` }),
          });
        }
        if (u.includes('/stop')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({}),
            text: async () => JSON.stringify({}),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({}),
          text: async () => JSON.stringify({}),
        });
      });
      vi.stubGlobal('fetch', fetchMock);

      let exposedActions!: any;

      function FacadeRetryHarness() {
        const containerRef = useRef<HTMLDivElement>(null);
        const videoRef = useRef<VideoElementRef>(null);
        const hlsRef = useRef<HlsInstanceRef>(null);
        const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

        const { playbackState, actions } = usePlaybackOrchestrator(
          {
            autoStart: false,
            sRef: '1:0:1:RETRY',
            onClose: () => {
              onCloseCalled = true;
            },
          } as unknown as V3PlayerProps,
          { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
        );
        exposedActions = actions;

        return (
          <div>
            <span data-testid="status">{playbackState.status}</span>
          </div>
        );
      }

      render(<FacadeRetryHarness />);

      // Initial start
      await act(async () => {
        await exposedActions.startStream('1:0:1:RETRY');
      });
      expect(streamInfoCalls).toBe(1);

      // Call actions.retry()
      let retryResult: any;
      await act(async () => {
        retryResult = await exposedActions.retry();
      });

      expect(retryResult.status).toBe('restarted');
      expect(onCloseCalled).toBe(false);
      // streamInfo called again for the restart
      expect(streamInfoCalls).toBe(2);
    });

    it('cancels retry when actions.stopStream is called while retry teardown is deferred', async () => {
      let streamInfoCalls = 0;
      let stopIntentEntered = false;
      let stopDeferredResolve!: () => void;
      const stopDeferred = new Promise<void>((resolve) => {
        stopDeferredResolve = resolve;
      });

      fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
        const u = String(url);
        if (u.includes('/live/stream-info')) {
          streamInfoCalls += 1;
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              mode: 'direct_stream',
              playbackDecisionToken: `token-stop-${streamInfoCalls}`,
              decision: { mode: 'direct_stream', playbackDecisionToken: `token-stop-${streamInfoCalls}` },
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/intents')) {
          const bodyStr = typeof init?.body === 'string' ? init.body : '';
          if (bodyStr.includes('stream.stop')) {
            stopIntentEntered = true;
            return stopDeferred.then(() => ({
              ok: true,
              status: 200,
              headers: new Headers(),
              json: async () => ({}),
              text: async () => JSON.stringify({}),
            }));
          }
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({ sessionId: `sess-stop-${streamInfoCalls}` }),
            text: async () => JSON.stringify({ sessionId: `sess-stop-${streamInfoCalls}` }),
          });
        }
        if (u.includes('/sessions/sess-stop-')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              sessionId: `sess-stop-${streamInfoCalls}`,
              state: 'READY',
              mode: 'LIVE',
              playbackUrl: `http://test/sess-stop-${streamInfoCalls}.m3u8`,
              heartbeatIntervalSeconds: 5,
              leaseExpiresAt: '2026-09-09T22:00:00Z',
            }),
            text: async () => JSON.stringify({}),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({}),
          text: async () => JSON.stringify({}),
        });
      });
      vi.stubGlobal('fetch', fetchMock);

      let exposedController!: any;
      let exposedActions!: any;

      function FacadeStopCancelHarness() {
        const containerRef = useRef<HTMLDivElement>(null);
        const videoRef = useRef<VideoElementRef>(null);
        const hlsRef = useRef<HlsInstanceRef>(null);
        const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

        const { controller, actions } = usePlaybackOrchestrator(
          { autoStart: false, sRef: '1:0:1:STOP' } as unknown as V3PlayerProps,
          { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
        );
        exposedController = controller;
        exposedActions = actions;

        return <div />;
      }

      render(<FacadeStopCancelHarness />);

      await act(async () => {
        await exposedActions.startStream('1:0:1:STOP');
      });
      expect(streamInfoCalls).toBe(1);

      // Begin retry (awaits deferred stop)
      let retryPromise: Promise<any>;
      act(() => {
        retryPromise = exposedActions.retry();
      });

      // Wait for the asynchronous teardown to reach the backend stop intent in /intents
      await waitFor(() => {
        expect(stopIntentEntered).toBe(true);
      });

      expect(exposedController.isRetryInFlight()).toBe(true);

      // External user stop
      let stopPromise: Promise<void>;
      act(() => {
        stopPromise = exposedActions.stopStream();
      });

      // The retry promise MUST settle promptly with cancelled user_stop
      const retryResult = await retryPromise!;
      expect(retryResult).toEqual({ status: 'cancelled', reason: 'user_stop' });

      // Resolve the deferred backend stop
      stopDeferredResolve();
      await act(async () => {
        await stopPromise;
      });

      // Stream info must NOT have been called again (zero restarts)
      expect(streamInfoCalls).toBe(1);
    });

    it('routes real VOD retry request to recording playback', async () => {
      client.setConfig({ baseUrl: 'http://localhost/api/v3' });
      let vodPlaybackCalls = 0;
      const vodPayload = {
        recordingId: 'rec-vod-retry',
        mode: 'direct',
        playbackUrl: 'http://test/vod-retry.m3u8',
      };
      fetchMock = vi.fn().mockImplementation((input: any) => {
        const u = typeof input === 'string' ? input : (input?.url ?? String(input));
        if (u.includes('/recordings/rec-vod-retry/stream-info')) {
          vodPlaybackCalls += 1;
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers({ 'Content-Type': 'application/json' }),
            json: async () => vodPayload,
            text: async () => JSON.stringify(vodPayload),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers({ 'Content-Type': 'application/json' }),
          json: async () => ({}),
          text: async () => JSON.stringify({}),
        });
      });
      vi.stubGlobal('fetch', fetchMock);

      let exposedActions!: any;
      function VodHarness() {
        const containerRef = useRef<HTMLDivElement>(null);
        const videoRef = useRef<VideoElementRef>(null);
        const hlsRef = useRef<HlsInstanceRef>(null);
        const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

        const { actions } = usePlaybackOrchestrator(
          { autoStart: false, recordingId: 'rec-vod-retry' } as unknown as V3PlayerProps,
          { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
        );
        exposedActions = actions;
        return <div />;
      }

      render(<VodHarness />);

      await act(async () => {
        await exposedActions.startStream();
      });
      expect(vodPlaybackCalls).toBe(1);

      let retryResult: any;
      await act(async () => {
        retryResult = await exposedActions.retry();
      });
      expect(retryResult.status).toBe('restarted');
      expect(vodPlaybackCalls).toBe(2);
    });

    it('routes real direct src retry request', async () => {
      let exposedActions!: any;
      let exposedState!: any;
      function SrcHarness() {
        const containerRef = useRef<HTMLDivElement>(null);
        const videoRef = useRef<VideoElementRef>(null);
        const hlsRef = useRef<HlsInstanceRef>(null);
        const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

        const { actions, playbackState } = usePlaybackOrchestrator(
          { autoStart: false, src: 'https://test.example/live.m3u8' } as unknown as V3PlayerProps,
          { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
        );
        exposedActions = actions;
        exposedState = playbackState;
        return <div />;
      }

      render(<SrcHarness />);

      await act(async () => {
        await exposedActions.startStream();
      });
      expect(exposedState.playbackMode).toBe('LIVE');

      let retryResult: any;
      await act(async () => {
        retryResult = await exposedActions.retry();
      });
      expect(retryResult.status).toBe('restarted');
    });

    it('cancels retry as superseded when committed source changes during deferred teardown', async () => {
      let stopDeferredResolve!: () => void;
      const stopDeferred = new Promise<void>((resolve) => {
        stopDeferredResolve = resolve;
      });

      let channelACalls = 0;
      let stopIntentEntered = false;

      fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
        const u = String(url);
        const bodyStr = typeof init?.body === 'string' ? init.body : '';
        if (u.includes('/live/stream-info') && bodyStr.includes('1:0:1:SRC-A')) {
          channelACalls += 1;
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              mode: 'direct_stream',
              playbackDecisionToken: `token-A-${channelACalls}`,
              decision: { mode: 'direct_stream', playbackDecisionToken: `token-A-${channelACalls}` },
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/intents')) {
          const bodyStr = typeof init?.body === 'string' ? init.body : '';
          if (bodyStr.includes('stream.stop')) {
            stopIntentEntered = true;
            return stopDeferred.then(() => ({
              ok: true,
              status: 200,
              headers: new Headers(),
              json: async () => ({}),
              text: async () => JSON.stringify({}),
            }));
          }
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({ sessionId: 'sess-A' }),
            text: async () => JSON.stringify({ sessionId: 'sess-A' }),
          });
        }
        if (u.includes('/sessions/sess-A')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              sessionId: 'sess-A',
              state: 'READY',
              mode: 'LIVE',
              playbackUrl: 'http://test/sess-A.m3u8',
              heartbeatIntervalSeconds: 5,
              leaseExpiresAt: '2026-09-09T22:00:00Z',
            }),
            text: async () => JSON.stringify({}),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({}),
          text: async () => JSON.stringify({}),
        });
      });
      vi.stubGlobal('fetch', fetchMock);

      let exposedActions!: any;
      let exposedController!: any;
      function DynamicSourceHarness({ sRef }: { sRef: string }) {
        const containerRef = useRef<HTMLDivElement>(null);
        const videoRef = useRef<VideoElementRef>(null);
        const hlsRef = useRef<HlsInstanceRef>(null);
        const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

        const { actions, controller } = usePlaybackOrchestrator(
          { autoStart: false, sRef } as unknown as V3PlayerProps,
          { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
        );
        exposedActions = actions;
        exposedController = controller;
        return <div />;
      }

      const { rerender } = render(<DynamicSourceHarness sRef="1:0:1:SRC-A" />);

      await act(async () => {
        await exposedActions.startStream('1:0:1:SRC-A');
      });
      expect(channelACalls).toBe(1);

      // Begin retry for SRC-A (awaits deferred stop)
      let retryPromise: Promise<any>;
      act(() => {
        retryPromise = exposedActions.retry();
      });

      await waitFor(() => {
        expect(stopIntentEntered).toBe(true);
      });
      expect(exposedController.isRetryInFlight()).toBe(true);

      // Rerender with changed committed source prop SRC-B while teardown is deferred
      act(() => {
        rerender(<DynamicSourceHarness sRef="1:0:1:SRC-B" />);
      });

      // Retry promise must settle promptly with cancelled superseded
      const retryResult = await retryPromise!;
      expect(retryResult).toEqual({ status: 'cancelled', reason: 'superseded' });

      // Resolve the deferred stop teardown
      stopDeferredResolve();
      await act(async () => {});

      // SRC-A must NOT have been restarted
      expect(channelACalls).toBe(1);
    });

    it('executes retry correctly under React.StrictMode without duplicated commands', async () => {
      let streamInfoCalls = 0;
      fetchMock = vi.fn().mockImplementation((url: string) => {
        const u = String(url);
        if (u.includes('/live/stream-info')) {
          streamInfoCalls += 1;
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              mode: 'direct_stream',
              playbackDecisionToken: `token-strict-${streamInfoCalls}`,
              decision: { mode: 'direct_stream', playbackDecisionToken: `token-strict-${streamInfoCalls}` },
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/intents')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({ sessionId: `sess-strict-${streamInfoCalls}` }),
            text: async () => JSON.stringify({ sessionId: `sess-strict-${streamInfoCalls}` }),
          });
        }
        if (u.includes('/sessions/sess-strict-')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              sessionId: `sess-strict-${streamInfoCalls}`,
              state: 'READY',
              mode: 'LIVE',
              playbackUrl: 'http://test/strict.m3u8',
              heartbeatIntervalSeconds: 5,
              leaseExpiresAt: '2026-09-09T22:00:00Z',
            }),
            text: async () => JSON.stringify({}),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({}),
          text: async () => JSON.stringify({}),
        });
      });
      vi.stubGlobal('fetch', fetchMock);

      let exposedActions!: any;
      function StrictHarness() {
        const containerRef = useRef<HTMLDivElement>(null);
        const videoRef = useRef<VideoElementRef>(null);
        const hlsRef = useRef<HlsInstanceRef>(null);
        const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

        const { actions } = usePlaybackOrchestrator(
          { autoStart: false, sRef: '1:0:1:STRICT' } as unknown as V3PlayerProps,
          { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
        );
        exposedActions = actions;
        return <div />;
      }

      render(
        <StrictMode>
          <StrictHarness />
        </StrictMode>,
      );

      await act(async () => {
        await exposedActions.startStream('1:0:1:STRICT');
      });
      expect(streamInfoCalls).toBe(1);

      let retryResult: any;
      await act(async () => {
        retryResult = await exposedActions.retry();
      });
      expect(retryResult.status).toBe('restarted');
      expect(streamInfoCalls).toBe(2);
    });

    it('cancels retry as disposed when component unmounts during pending preparation', async () => {
      let releaseProbe!: (value: any) => void;
      const pendingProbe = new Promise<any>((resolve) => {
        releaseProbe = resolve;
      });
      vi.spyOn(networkProbeModule, 'measurePlaybackNetwork').mockResolvedValueOnce(undefined as any).mockReturnValue(pendingProbe);

      fetchMock = vi.fn().mockImplementation((url: string) => {
        const u = String(url);
        if (u.includes('/live/stream-info')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              mode: 'direct_stream',
              playbackDecisionToken: 'token-unmount',
              decision: { mode: 'direct_stream', playbackDecisionToken: 'token-unmount' },
            }),
            text: async () => JSON.stringify({}),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({}),
          text: async () => JSON.stringify({}),
        });
      });
      vi.stubGlobal('fetch', fetchMock);

      let exposedActions!: any;
      let exposedController!: any;
      function UnmountHarness() {
        const containerRef = useRef<HTMLDivElement>(null);
        const videoRef = useRef<VideoElementRef>(null);
        const hlsRef = useRef<HlsInstanceRef>(null);
        const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

        const { actions, controller } = usePlaybackOrchestrator(
          { autoStart: false, sRef: '1:0:1:UNMOUNT' } as unknown as V3PlayerProps,
          { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
        );
        exposedActions = actions;
        exposedController = controller;
        return <div />;
      }

      const view = render(<UnmountHarness />);

      await act(async () => {
        await exposedActions.startStream('1:0:1:UNMOUNT');
      });

      // Begin retry: its restart enters startStream and awaits pendingProbe
      act(() => {
        void exposedActions.retry();
      });

      // Retry preparation is in flight
      expect(exposedController.isRetryInFlight()).toBe(true);

      // Unmount while preparation is still pending
      act(() => {
        view.unmount();
      });

      // Controller is disposed and retry is cancelled
      expect(exposedController.isRetryInFlight()).toBe(false);

      // Clean up deferred probe
      releaseProbe(null);
    });
  });
});
