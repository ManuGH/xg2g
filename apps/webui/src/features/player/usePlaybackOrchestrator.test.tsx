import { createRef, useRef, useState } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { HlsInstanceRef, V3PlayerProps, VideoElementRef } from '../../types/v3-player';
import { usePlaybackOrchestrator } from './usePlaybackOrchestrator';
import * as networkProbeModule from './utils/playbackNetworkProbe';

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
  });
});
