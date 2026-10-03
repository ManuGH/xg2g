import { createRef, useRef, useState, StrictMode } from 'react';
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

    it('dispatches stop intent with keepalive: true on pagehide event (tab close / unload)', async () => {
      fetchMock = vi.fn().mockImplementation((url: string) => {
        const u = String(url);
        if (u.includes('/live/stream-info')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              mode: 'direct_stream',
              playbackDecisionToken: 'token-unload-1',
              decision: { mode: 'direct_stream', playbackDecisionToken: 'token-unload-1' },
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/intents')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({ sessionId: 'sess-unload-1' }),
            text: async () => JSON.stringify({ sessionId: 'sess-unload-1' }),
          });
        }
        if (u.includes('/sessions/sess-unload-1')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              sessionId: 'sess-unload-1',
              state: 'READY',
              mode: 'LIVE',
              playbackUrl: 'http://test/live.m3u8',
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

      function UnloadHarness() {
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
              start-live
            </button>
          </div>
        );
      }

      render(<UnloadHarness />);

      // Start Live -> wait until ready
      fireEvent.click(screen.getByRole('button', { name: 'start-live' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('ready');
      });

      // Trigger tab close / unload via pagehide
      act(() => {
        window.dispatchEvent(new Event('pagehide'));
      });

      // Synchronously initiated during pagehide before any await, microtask flush, or timer advancement
      const intentCalls = fetchMock.mock.calls.filter((c: any[]) => String(c[0]).includes('/intents'));
      const stopCalls = intentCalls.filter((c: any[]) => {
        const body = c[1]?.body ? JSON.parse(String(c[1].body)) : {};
        return body?.type === 'stream.stop' && body?.sessionId === 'sess-unload-1';
      });
      expect(stopCalls).toHaveLength(1);
      const stopCall = stopCalls[0]!;
      expect(stopCall[1]?.keepalive).toBe(true);
    });
  });

  describe('Unload effect lifecycle matrix (Mount, Rerender, Executor change, Unmount, StrictMode)', () => {
    let addListenerSpy: any;
    let removeListenerSpy: any;
    let fetchMock: any;

    beforeEach(() => {
      vi.clearAllMocks();
      vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined as never);
      vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});

      addListenerSpy = vi.spyOn(window, 'addEventListener');
      removeListenerSpy = vi.spyOn(window, 'removeEventListener');

      fetchMock = vi.fn().mockImplementation((url: string) => {
        const u = String(url);
        if (u.includes('/live/stream-info')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              mode: 'direct_stream',
              playbackDecisionToken: 'token-lm-1',
              decision: { mode: 'direct_stream', playbackDecisionToken: 'token-lm-1' },
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/intents')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({ sessionId: 'sess-lm-1' }),
            text: async () => JSON.stringify({ sessionId: 'sess-lm-1' }),
          });
        }
        if (u.includes('/sessions/sess-lm-1')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              sessionId: 'sess-lm-1',
              state: 'READY',
              mode: 'LIVE',
              playbackUrl: 'http://test/live.m3u8',
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
    });

    afterEach(() => {
      vi.restoreAllMocks();
    });

    function LifecycleHarness({ count = 0, onStop, onPause }: { count?: number; onStop?: () => void; onPause?: () => void }) {
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
          <span data-testid="count">{count}</span>
          <button onClick={() => void actions.startStream('1:0:1:AA')} type="button">
            start
          </button>
          <button onClick={() => { onStop?.(); void actions.stopStream(); }} type="button">
            stop
          </button>
          <button onClick={() => { onPause?.(); actions.togglePlayPause(); }} type="button">
            pause
          </button>
        </div>
      );
    }

    it('maintains exactly one active pagehide listener and registers NO beforeunload listener across rerenders', () => {
      const { rerender } = render(<LifecycleHarness count={0} onStop={() => {}} />);

      const getActivePagehideListeners = () => {
        const adds = addListenerSpy.mock.calls.filter((c: any[]) => c[0] === 'pagehide');
        const removes = removeListenerSpy.mock.calls.filter((c: any[]) => c[0] === 'pagehide');
        return adds.length - removes.length;
      };

      const getActiveBeforeunloadListeners = () => {
        const adds = addListenerSpy.mock.calls.filter((c: any[]) => c[0] === 'beforeunload');
        const removes = removeListenerSpy.mock.calls.filter((c: any[]) => c[0] === 'beforeunload');
        return adds.length - removes.length;
      };

      // beforeunload must NOT be registered (prevents spurious stop on mailto/downloads)
      expect(getActiveBeforeunloadListeners()).toBe(0);
      // pagehide is registered by useDocumentVisibility (1) and unload stop effect (1) = 2 total
      expect(getActivePagehideListeners()).toBe(2);

      // Rerender with changed props (count): listener count must remain stable (no leaks / re-registrations)
      rerender(<LifecycleHarness count={1} onStop={() => {}} />);
      expect(getActiveBeforeunloadListeners()).toBe(0);
      expect(getActivePagehideListeners()).toBe(2);

      // Rerender with changed callback: listener count must remain stable
      rerender(<LifecycleHarness count={2} onStop={() => {}} />);
      expect(getActiveBeforeunloadListeners()).toBe(0);
      expect(getActivePagehideListeners()).toBe(2);
    });

    it('removes listeners on unmount and ignores pagehide after unmount', () => {
      const { unmount } = render(<LifecycleHarness />);

      const adds = addListenerSpy.mock.calls.filter((c: any[]) => c[0] === 'pagehide');
      expect(adds.length).toBeGreaterThanOrEqual(1);

      unmount();

      const pagehideAdds = addListenerSpy.mock.calls.filter((c: any[]) => c[0] === 'pagehide');
      const pagehideRemoves = removeListenerSpy.mock.calls.filter((c: any[]) => c[0] === 'pagehide');
      expect(pagehideAdds.length).toBe(pagehideRemoves.length);

      const beforeunloadAdds = addListenerSpy.mock.calls.filter((c: any[]) => c[0] === 'beforeunload');
      expect(beforeunloadAdds).toHaveLength(0);

      // Dispatch pagehide after unmount -> no stop intents sent
      act(() => {
        window.dispatchEvent(new Event('pagehide'));
      });

      const intentCalls = fetchMock.mock.calls.filter((c: any[]) => String(c[0]).includes('/intents'));
      const stopCalls = intentCalls.filter((c: any[]) => {
        const body = c[1]?.body ? JSON.parse(String(c[1].body)) : {};
        return body?.type === 'stream.stop';
      });
      expect(stopCalls).toHaveLength(0);
    });

    it('preserves single listener and dispatches no duplicate stop under StrictMode remount', async () => {
      render(
        <StrictMode>
          <LifecycleHarness />
        </StrictMode>,
      );

      // Under StrictMode double-invocation: beforeunload is never registered
      const beforeunloadAdds = addListenerSpy.mock.calls.filter((c: any[]) => c[0] === 'beforeunload');
      expect(beforeunloadAdds).toHaveLength(0);

      // pagehide has 2 active listeners (1 visibility + 1 unload effect)
      const pagehideAdds = addListenerSpy.mock.calls.filter((c: any[]) => c[0] === 'pagehide');
      const pagehideRemoves = removeListenerSpy.mock.calls.filter((c: any[]) => c[0] === 'pagehide');
      expect(pagehideAdds.length - pagehideRemoves.length).toBe(2);

      // Start stream and wait until ready
      fireEvent.click(screen.getByRole('button', { name: 'start' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('ready');
      });

      // Dispatch pagehide
      act(() => {
        window.dispatchEvent(new Event('pagehide'));
      });

      // Exactly ONE stop intent must be dispatched with keepalive: true
      const intentCalls = fetchMock.mock.calls.filter((c: any[]) => String(c[0]).includes('/intents'));
      const stopCalls = intentCalls.filter((c: any[]) => {
        const body = c[1]?.body ? JSON.parse(String(c[1].body)) : {};
        return body?.type === 'stream.stop' && body?.sessionId === 'sess-lm-1';
      });
      expect(stopCalls).toHaveLength(1);
      expect(stopCalls[0]![1]?.keepalive).toBe(true);
    });

    it('does not dispatch stop intent on beforeunload event (protects against mailto/downloads/cancelled leave)', async () => {
      render(<LifecycleHarness />);

      fireEvent.click(screen.getByRole('button', { name: 'start' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('ready');
      });

      // Dispatch beforeunload (e.g. user clicked mailto: link or cancelled leave dialog)
      act(() => {
        window.dispatchEvent(new Event('beforeunload'));
      });

      const intentCalls = fetchMock.mock.calls.filter((c: any[]) => String(c[0]).includes('/intents'));
      const stopCalls = intentCalls.filter((c: any[]) => {
        const body = c[1]?.body ? JSON.parse(String(c[1].body)) : {};
        return body?.type === 'stream.stop';
      });
      expect(stopCalls).toHaveLength(0);
    });

    it('dispatches stop intent with keepalive on pagehide when stream is paused (prevents Vu+ tuner starvation and warning popup)', async () => {
      render(<LifecycleHarness />);

      fireEvent.click(screen.getByRole('button', { name: 'start' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('ready');
      });

      // Pause the stream
      fireEvent.click(screen.getByRole('button', { name: 'pause' }));

      // Dispatch pagehide while paused
      act(() => {
        window.dispatchEvent(new Event('pagehide'));
      });

      // Stop intent MUST be dispatched with keepalive: true to immediately release the tuner on the Vu+
      const intentCalls = fetchMock.mock.calls.filter((c: any[]) => String(c[0]).includes('/intents'));
      const stopCalls = intentCalls.filter((c: any[]) => {
        const body = c[1]?.body ? JSON.parse(String(c[1].body)) : {};
        return body?.type === 'stream.stop' && body?.sessionId === 'sess-lm-1';
      });
      expect(stopCalls).toHaveLength(1);
      expect(stopCalls[0]![1]?.keepalive).toBe(true);
    });

    it('dispatches stop intent with keepalive: false on regular in-app stopStream', async () => {
      render(<LifecycleHarness />);

      fireEvent.click(screen.getByRole('button', { name: 'start' }));
      await waitFor(() => {
        expect(screen.getByTestId('status')).toHaveTextContent('ready');
      });

      // Regular in-app stop (e.g. user clicks Stop button)
      fireEvent.click(screen.getByRole('button', { name: 'stop' }));

      await waitFor(() => {
        const intentCalls = fetchMock.mock.calls.filter((c: any[]) => String(c[0]).includes('/intents'));
        const stopCalls = intentCalls.filter((c: any[]) => {
          const body = c[1]?.body ? JSON.parse(String(c[1].body)) : {};
          return body?.type === 'stream.stop' && body?.sessionId === 'sess-lm-1';
        });
        expect(stopCalls).toHaveLength(1);
        expect(stopCalls[0]![1]?.keepalive).toBe(false);
      });
    });
  });
});
