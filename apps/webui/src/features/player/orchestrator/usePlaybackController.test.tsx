// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { startTransition, StrictMode, Suspense, useEffect, useLayoutEffect, useMemo, useState } from 'react';
import { act, render, renderHook, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { usePlaybackController } from './usePlaybackController';
import { createDefaultLiveSessionTransport, type LiveSessionTransport } from './liveSessionTransport';
import type { PlaybackCommand, PlaybackDomainState } from './playbackTypes';
import { createRecoveryLadderState } from './recoveryLadder';

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

function createDummyTransport(): LiveSessionTransport {
  return {
    fetchStreamInfo: vi.fn().mockResolvedValue({
      status: 200,
      data: {
        mode: 'direct_stream',
        playbackDecisionToken: 'token-default',
        decision: { mode: 'direct_stream', playbackDecisionToken: 'token-default' },
      },
      headers: new Headers(),
    }),
    postStartIntent: vi.fn().mockResolvedValue({ status: 200, data: { sessionId: 's1' }, headers: new Headers() }),
    waitForReady: vi.fn().mockResolvedValue({
      sessionId: 's1',
      playbackUrl: 'http://test',
      heartbeatIntervalSeconds: 5,
      leaseExpiresAt: '2026-09-07T22:00:00Z',
    }),
    postStopIntent: vi.fn().mockResolvedValue(undefined),
    postHeartbeat: vi.fn().mockResolvedValue({
      status: 200,
      data: { acknowledged: true, sessionId: 's1', leaseExpiresAt: '2026-09-07T22:05:00Z' },
      headers: new Headers(),
    }),
    fetchSessionSnapshot: vi.fn().mockResolvedValue({
      status: 200,
      data: { sessionId: 's1', state: 'READY' },
      headers: new Headers(),
    }),
  };
}

describe('usePlaybackController React Integration & Lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('runs commands synchronously upon dispatch', () => {
    const transport = createDummyTransport();
    const executed: PlaybackCommand[] = [];

    function TestComponent() {
      const { dispatch } = usePlaybackController(
        transport,
        createInitialState,
        (command) => executed.push(command),
      );

      return (
        <button
          onClick={() =>
            dispatch({
              type: 'intent.stop.requested',
              epoch: 1,
              reason: 'user_stop',
              notifyClose: false,
            })
          }
        >
          Stop
        </button>
      );
    }

    render(<TestComponent />);

    expect(executed).toHaveLength(0);

    screen.getByRole('button', { name: 'Stop' }).click();

    // Commands must have run immediately and synchronously
    expect(executed.length).toBeGreaterThanOrEqual(1);
    expect(executed[0]!.type).toBe('command.timeline.end_attempt');
  });

  it('preserves executor across StrictMode double-mount simulation and executes child layout effect', () => {
    const transport = createDummyTransport();
    const executed: PlaybackCommand[] = [];

    function ChildComponent({ dispatch }: { dispatch: (e: any) => void }) {
      useLayoutEffect(() => {
        dispatch({
          type: 'intent.stop.requested',
          epoch: 1,
          reason: 'user_stop',
          notifyClose: false,
        });
      }, [dispatch]);

      return <div>Child</div>;
    }

    function ParentComponent() {
      const { dispatch } = usePlaybackController(
        transport,
        createInitialState,
        (command) => executed.push(command),
      );

      return <ChildComponent dispatch={dispatch} />;
    }

    render(
      <StrictMode>
        <ParentComponent />
      </StrictMode>,
    );

    // In StrictMode, child's useLayoutEffect runs on mount, unmounts, and runs on remount.
    // Each dispatch of intent.stop.requested produces 3 teardown commands (6 total).
    // Both setups must execute synchronously.
    expect(executed).toHaveLength(6);
    expect(executed[0]!.type).toBe('command.timeline.end_attempt');
    expect(executed[3]!.type).toBe('command.timeline.end_attempt');
  });

  it('updates command executor dynamically without losing controller instance', () => {
    const transport = createDummyTransport();
    const firstLog: string[] = [];
    const secondLog: string[] = [];

    function TestComponent({ useSecond }: { useSecond: boolean }) {
      const { dispatch } = usePlaybackController(
        transport,
        createInitialState,
        useSecond
          ? () => secondLog.push('second')
          : () => firstLog.push('first'),
      );

      return (
        <button
          onClick={() =>
            dispatch({
              type: 'intent.stop.requested',
              epoch: 1,
              reason: 'user_stop',
              notifyClose: false,
            })
          }
        >
          Dispatch
        </button>
      );
    }

    const { rerender } = render(<TestComponent useSecond={false} />);
    screen.getByRole('button', { name: 'Dispatch' }).click();
    expect(firstLog).toEqual(['first', 'first', 'first']);
    expect(secondLog).toEqual([]);

    // Rerender with new executor
    rerender(<TestComponent useSecond={true} />);
    screen.getByRole('button', { name: 'Dispatch' }).click();
    expect(firstLog).toEqual(['first', 'first', 'first']);
    expect(secondLog).toEqual(['second', 'second', 'second']);
  });

  it('calls controller.dispose on real component unmount', () => {
    const transport = createDummyTransport();
    let controllerRef: any = null;

    function TestComponent() {
      const { controller } = usePlaybackController(
        transport,
        createInitialState,
        () => {},
      );
      controllerRef = controller;
      return <div>Active</div>;
    }

    const { unmount } = render(<TestComponent />);

    const disposeSpy = vi.spyOn(controllerRef, 'dispose');

    expect(disposeSpy).not.toHaveBeenCalled();

    unmount();

    // Real unmount must call controller.dispose()!
    expect(disposeSpy).toHaveBeenCalledTimes(1);
  });

  it('preserves controller across Suspense hiding and revealing when owner is inside boundary', async () => {
    const transport = createDummyTransport();
    let controllerRef: any = null;
    let setSuspendedFn: ((s: boolean) => void) | null = null;
    const executed: PlaybackCommand[] = [];

    let resolveSuspendingPromise: () => void;
    const suspendingPromise = new Promise<void>((r) => {
      resolveSuspendingPromise = r;
    });

    function SuspendingOwner({ suspended }: { suspended: boolean }) {
      const { controller, dispatch } = usePlaybackController(
        transport,
        createInitialState,
        (cmd) => executed.push(cmd),
      );
      controllerRef = controller;

      if (suspended) {
        throw suspendingPromise;
      }

      return (
        <div>
          <span>Content</span>
          <button
            onClick={() =>
              dispatch({
                type: 'intent.stop.requested',
                epoch: 1,
                reason: 'user_stop',
                notifyClose: false,
              })
            }
          >
            Stop
          </button>
        </div>
      );
    }

    function App() {
      const [suspended, setSuspended] = useState(false);
      setSuspendedFn = setSuspended;

      return (
        <Suspense fallback={<div>Loading...</div>}>
          <SuspendingOwner suspended={suspended} />
        </Suspense>
      );
    }

    render(<App />);

    const disposeSpy = vi.spyOn(controllerRef, 'dispose');

    expect(screen.getByText('Content')).toBeInTheDocument();

    // Suspend: SuspendingOwner throws inside Suspense
    act(() => {
      setSuspendedFn!(true);
    });

    expect(screen.getByText('Loading...')).toBeInTheDocument();
    // Suspense hiding must NOT dispose the controller!
    expect(disposeSpy).not.toHaveBeenCalled();

    // Reveal
    await act(async () => {
      resolveSuspendingPromise();
      setSuspendedFn!(false);
    });

    expect(screen.getByText('Content')).toBeInTheDocument();
    expect(disposeSpy).not.toHaveBeenCalled();

    // Dispatching after reveal must execute commands synchronously
    screen.getByRole('button', { name: 'Stop' }).click();
    expect(executed.length).toBeGreaterThanOrEqual(1);
  });

  it('allows startLive to succeed after StrictMode mount simulation is fully flushed', async () => {
    const transport = createDummyTransport();
    let controllerRef: any = null;

    function TestComponent() {
      const { controller } = usePlaybackController(
        transport,
        createInitialState,
        () => {},
      );
      controllerRef = controller;
      return <div>Mounted</div>;
    }

    render(
      <StrictMode>
        <TestComponent />
      </StrictMode>,
    );

    expect(screen.getByText('Mounted')).toBeInTheDocument();
    expect(controllerRef).not.toBeNull();

    // After StrictMode mount is flushed, controller must be active (not disposed)
    // startLive must proceed rather than immediately cancelling with user_stop
    let result: any;
    await act(async () => {
      result = await controllerRef.startLive({ serviceRef: 'live-1' });
    });

    expect(result.status).toBe('ready');
    expect(result.sessionId).toBe('s1');
  });

  it('drops commands when saved dispatch is invoked after real component unmount', () => {
    const transport = createDummyTransport();
    const executed: PlaybackCommand[] = [];
    let savedDispatch: any = null;

    function TestComponent() {
      const { dispatch } = usePlaybackController(
        transport,
        createInitialState,
        (cmd) => executed.push(cmd),
      );
      savedDispatch = dispatch;
      return <div>Active</div>;
    }

    const { unmount } = render(<TestComponent />);
    expect(savedDispatch).not.toBeNull();

    // Unmount the component
    unmount();

    // Calling saved dispatch after unmount must execute 0 commands
    savedDispatch({
      type: 'intent.stop.requested',
      epoch: 1,
      reason: 'user_stop',
      notifyClose: false,
    });

    expect(executed).toHaveLength(0);
  });

  it('handles startLive initiated inside effect across StrictMode double-mount simulation', async () => {
    let callCount = 0;
    const stopCalls: string[] = [];
    const transport: LiveSessionTransport = {
      fetchStreamInfo: vi.fn().mockResolvedValue({
        status: 200,
        data: {
          mode: 'direct_stream',
          playbackDecisionToken: 'token-default',
          decision: { mode: 'direct_stream', playbackDecisionToken: 'token-default' },
        },
        headers: new Headers(),
      }),
      postStartIntent: vi.fn().mockImplementation(async () => {
        callCount++;
        return {
          status: 200,
          data: { sessionId: `s${callCount}` },
          headers: new Headers(),
        };
      }),
      waitForReady: vi.fn().mockImplementation(async ({ sessionId }) => ({
        sessionId,
        playbackUrl: `http://test/${sessionId}.m3u8`,
        heartbeatIntervalSeconds: 5,
        leaseExpiresAt: '2026-09-07T22:00:00Z',
      })),
      postStopIntent: vi.fn().mockImplementation(async ({ sessionId }) => {
        stopCalls.push(sessionId);
      }),
    };

    let controllerRef: any = null;

    function TestComponent() {
      const { controller } = usePlaybackController(
        transport,
        createInitialState,
        () => {},
      );
      controllerRef = controller;

      useEffect(() => {
        void controller.startLive({ serviceRef: 'live-channel' });
      }, [controller]);

      return <div>Active</div>;
    }

    await act(async () => {
      render(
        <StrictMode>
          <TestComponent />
        </StrictMode>,
      );
    });

    expect(controllerRef).not.toBeNull();
    // In React StrictMode, effect runs in pass 1 and is immediately torn down by cleanup 1 (dispose),
    // cleanly aborting pass 1 during preflight before start intent network request.
    // Pass 2 runs after activate(), establishing the active live session without leaks or orphan collisions.
    expect(callCount).toBe(1);
    expect(controllerRef.getActiveSessionId()).toBe('s1');
    expect(controllerRef.getInFlightStartsCount()).toBe(0);
  });

  it('handles in-flight session when component unmounts and re-mounts while waiting for ready', async () => {
    let callCount = 0;
    const stopCalls: string[] = [];
    let resolveReadyS1!: (value: any) => void;
    const readyS1Promise = new Promise((resolve) => {
      resolveReadyS1 = resolve;
    });

    const transport: LiveSessionTransport = {
      fetchStreamInfo: vi.fn().mockResolvedValue({
        status: 200,
        data: {
          mode: 'direct_stream',
          playbackDecisionToken: 'token-default',
          decision: { mode: 'direct_stream', playbackDecisionToken: 'token-default' },
        },
        headers: new Headers(),
      }),
      postStartIntent: vi.fn().mockImplementation(async () => {
        callCount++;
        return {
          status: 200,
          data: { sessionId: `s${callCount}` },
          headers: new Headers(),
        };
      }),
      waitForReady: vi.fn().mockImplementation(async ({ sessionId }) => {
        if (sessionId === 's1') {
          await readyS1Promise;
        }
        return {
          sessionId,
          playbackUrl: `http://test/${sessionId}.m3u8`,
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: '2026-09-07T22:00:00Z',
        };
      }),
      postStopIntent: vi.fn().mockImplementation(async ({ sessionId }) => {
        stopCalls.push(sessionId);
      }),
    };

    let controllerRef: any = null;

    function TestComponent() {
      const { controller } = usePlaybackController(
        transport,
        createInitialState,
        () => {},
      );
      controllerRef = controller;

      useEffect(() => {
        void controller.startLive({ serviceRef: 'live-channel' });
      }, [controller]);

      return <div>Active</div>;
    }

    let unmountFn!: () => void;
    await act(async () => {
      const rendered = render(<TestComponent />);
      unmountFn = rendered.unmount;
    });

    // Wait for pass 1 to execute preflight and postStartIntent
    await vi.waitFor(() => {
      expect(callCount).toBe(1);
    });

    // Unmount the component while pass 1 is waiting for ready: controller.dispose() is called
    await act(async () => {
      unmountFn();
      // Now s1 was adopted or in-flight when unmount happened; resolve its ready promise
      resolveReadyS1({});
    });

    // Re-mount the component: controller.activate() and new pass starts
    await act(async () => {
      render(<TestComponent />);
    });

    await vi.waitFor(() => {
      expect(callCount).toBe(2);
      expect(controllerRef.getActiveSessionId()).toBe('s2');
    });

    expect(controllerRef.getInFlightStartsCount()).toBe(0);
    // Orphan session s1 must have been stopped
    expect(stopCalls).toContain('s1');
  });

  it('manages heartbeat lifecycle across StrictMode mount/unmount and snapshot callbacks without duplicate timers', async () => {
    vi.useFakeTimers();
    try {
      const heartbeatCalls: string[] = [];
      const snapshotsReceived: any[] = [];

      const transport: LiveSessionTransport = {
        fetchStreamInfo: vi.fn().mockResolvedValue({
          status: 200,
          data: { mode: 'direct_stream', playbackDecisionToken: 'token' },
          headers: new Headers(),
        }),
        postStartIntent: vi.fn().mockResolvedValue({
          status: 200,
          data: { sessionId: 'session-strict' },
          headers: new Headers(),
        }),
        waitForReady: vi.fn().mockResolvedValue({
          sessionId: 'session-strict',
          playbackUrl: 'http://test/stream.m3u8',
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: '2026-09-09T10:00:00Z',
        }),
        postStopIntent: vi.fn().mockResolvedValue(undefined),
        postHeartbeat: vi.fn().mockImplementation(async ({ sessionId }) => {
          heartbeatCalls.push(sessionId);
          return {
            status: 200,
            data: {
              acknowledged: true,
              sessionId,
              leaseExpiresAt: '2026-09-09T10:05:00Z',
            },
            headers: new Headers(),
          };
        }),
        fetchSessionSnapshot: vi.fn().mockImplementation(async ({ sessionId }) => ({
          status: 200,
          data: { sessionId, state: 'READY', bitrate: 5000 },
          headers: new Headers(),
        })),
      };

      let latestController: any = null;

      function StrictComponent() {
        const { controller, state } = usePlaybackController(
          transport,
          createInitialState,
          () => {},
          {
            onSessionSnapshot: (snapshot) => {
              snapshotsReceived.push(snapshot);
            },
          },
        );
        latestController = controller;

        useEffect(() => {
          void controller.startLive({ serviceRef: 'live-channel' });
        }, [controller]);

        return <div>Lease: {state.leaseExpiresAt ?? 'none'}</div>;
      }

      let unmountFn!: () => void;
      await act(async () => {
        const rendered = render(
          <StrictMode>
            <StrictComponent />
          </StrictMode>,
        );
        unmountFn = rendered.unmount;
      });

      // StartLive settles
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(latestController.getActiveSessionId()).toBe('session-strict');
      expect(latestController.isHeartbeatSupervising()).toBe(true);

      // Advance 5s -> exactly 1 heartbeat request fires (no duplicate timers from StrictMode double mount)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000);
      });
      expect(heartbeatCalls).toEqual(['session-strict']);

      // Snapshot callback was routed
      expect(snapshotsReceived.length).toBeGreaterThanOrEqual(1);

      // Unmount -> controller.dispose() cleans up heartbeat supervision
      await act(async () => {
        unmountFn();
      });
      expect(latestController.isHeartbeatSupervising()).toBe(false);

      // Advance another 10s: no more beats fire
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10000);
      });
      expect(heartbeatCalls).toHaveLength(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('uses refreshed credentials for an established session after a same-endpoint rerender', async () => {
    vi.useFakeTimers();
    try {
      const expiry = '2026-09-09T12:10:00Z';
      const fetchFn = vi.fn().mockImplementation(async () => new Response(JSON.stringify({
        acknowledged: true, sessionId: 'A', leaseExpiresAt: expiry,
      }), { status: 200, headers: { 'Content-Type': 'application/json' } }));

      const { result: hook, rerender, unmount } = renderHook(({ token }) => {
        const t = useMemo(() => {
          const real = createDefaultLiveSessionTransport({
            apiBase: 'https://example.test/api/v3',
            authHeaders: () => ({ Authorization: `Bearer ${token}` }),
            fetchFn,
          });
          return {
            ...createDummyTransport(),
            postHeartbeat: real.postHeartbeat,
          };
        }, [token]);
        return usePlaybackController(t, createInitialState, vi.fn());
      }, { initialProps: { token: 'fixture-old' } });

      await act(async () => {
        await hook.current.controller.startLive({ serviceRef: 'channel-A' });
      });

      rerender({ token: 'fixture-new' });

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000);
      });

      expect(fetchFn).toHaveBeenCalledTimes(1);
      const call = fetchFn.mock.calls[0];
      expect(call).toBeDefined();
      expect(new Headers(call![1]?.headers).get('Authorization')).toBe('Bearer fixture-new');
      unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it('cleans up active timers and aborts in-flight requests on unmount after rerender', async () => {
    vi.useFakeTimers();
    try {
      const signals: AbortSignal[] = [];
      const fetchFn = vi.fn().mockImplementation(async ({ signal }: { signal: AbortSignal }) => {
        signals.push(signal);
        return new Promise(() => {}); // hang
      });

      const { result: hook, rerender, unmount } = renderHook(({ token }) => {
        const t = useMemo(() => {
          const real = createDefaultLiveSessionTransport({
            apiBase: 'https://example.test/api/v3',
            authHeaders: () => ({ Authorization: `Bearer ${token}` }),
            fetchFn,
          });
          return {
            ...createDummyTransport(),
            postHeartbeat: real.postHeartbeat,
          };
        }, [token]);
        return usePlaybackController(t, createInitialState, vi.fn());
      }, { initialProps: { token: 'fixture-old' } });

      await act(async () => {
        await hook.current.controller.startLive({ serviceRef: 'channel-A' });
      });

      rerender({ token: 'fixture-new' });

      // Unmount after rerender
      await act(async () => {
        unmount();
      });

      expect(hook.current.controller.isHeartbeatSupervising()).toBe(false);

      // Advance timers: no delayed beats should be invoked
      await act(async () => {
        await vi.advanceTimersByTimeAsync(15000);
      });
      expect(fetchFn).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it('preserves command executor across Suspense hide and reveal', async () => {
    const executed: PlaybackCommand[] = [];
    const transport = createDummyTransport();

    let toggleSuspense!: (suspend: boolean) => void;
    let promiseToSuspend: Promise<void> | null = null;
    let resolveSuspense!: () => void;

    function SuspendingChild({ suspend }: { suspend: boolean }) {
      if (suspend && promiseToSuspend) {
        throw promiseToSuspend;
      }
      const { controller } = usePlaybackController(
        transport,
        createInitialState,
        (cmd) => executed.push(cmd),
      );

      return (
        <button
          onClick={() =>
            controller.setCommandExecutor((cmd) => executed.push(cmd))
          }
        >
          Active
        </button>
      );
    }

    function ParentComponent() {
      const [suspend, setSuspend] = useState(false);
      toggleSuspense = setSuspend;

      return (
        <Suspense fallback={<div>Suspended</div>}>
          <SuspendingChild suspend={suspend} />
        </Suspense>
      );
    }

    render(<ParentComponent />);
    expect(screen.getByText('Active')).toBeDefined();

    // Trigger suspense
    promiseToSuspend = new Promise<void>((r) => { resolveSuspense = r; });
    act(() => {
      toggleSuspense(true);
    });
    expect(screen.getByText('Suspended')).toBeDefined();

    // Reveal from suspense
    await act(async () => {
      resolveSuspense();
      toggleSuspense(false);
    });
    expect(screen.getByText('Active')).toBeDefined();
  });

  it('does not publish credentials from a suspended uncommitted render to active heartbeats', async () => {
    vi.useFakeTimers();
    try {
      const response = (data: unknown) => ({ status: 200, data, headers: new Headers() });
      const expiry = '2026-09-09T12:10:00Z';
      const oldTransport: LiveSessionTransport = {
        apiBase: 'https://example.test/api/v3',
        fetchStreamInfo: vi.fn().mockResolvedValue(response({
          mode: 'direct_stream', playbackDecisionToken: 'fixture',
          decision: { mode: 'direct_stream', playbackDecisionToken: 'fixture' },
        })),
        postStartIntent: vi.fn().mockResolvedValue(response({ sessionId: 'A' })),
        waitForReady: vi.fn().mockResolvedValue({
          sessionId: 'A', playbackUrl: 'https://example.test/live.m3u8',
          heartbeatIntervalSeconds: 5, leaseExpiresAt: expiry,
        }),
        postStopIntent: vi.fn().mockResolvedValue(undefined),
        postHeartbeat: vi.fn().mockResolvedValue(response({
          acknowledged: true, sessionId: 'A', leaseExpiresAt: expiry,
        })),
      };
      const newTransport: LiveSessionTransport = {
        ...oldTransport,
        postHeartbeat: vi.fn().mockResolvedValue(response({
          acknowledged: true, sessionId: 'A', leaseExpiresAt: expiry,
        })),
      };
      const pending = new Promise(() => {});
      let controller!: ReturnType<typeof usePlaybackController>['controller'];
      let setPending!: () => void;
      function Owner({ version }: { version: number }) {
        const hook = usePlaybackController(version ? newTransport : oldTransport, createInitialState, vi.fn());
        if (version) throw pending;
        controller = hook.controller;
        return <div>committed-old</div>;
      }
      function Host() {
        const [version, setVersion] = useState(0);
        setPending = () => startTransition(() => setVersion(1));
        return <Suspense fallback={<div>pending</div>}><Owner version={version} /></Suspense>;
      }
      render(<Host />);
      await act(async () => { await controller.startLive({ serviceRef: 'channel-A' }); });
      await act(async () => { setPending(); });
      expect(screen.getByText('committed-old')).toBeTruthy();
      await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
      expect(vi.mocked(oldTransport.postHeartbeat!).mock.calls.length + vi.mocked(newTransport.postHeartbeat!).mock.calls.length).toBe(1);
      expect(oldTransport.postHeartbeat).toHaveBeenCalledTimes(1);
      expect(newTransport.postHeartbeat).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });
});
