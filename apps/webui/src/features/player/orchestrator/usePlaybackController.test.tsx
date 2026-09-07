// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { StrictMode, Suspense, useLayoutEffect, useState } from 'react';
import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { usePlaybackController } from './usePlaybackController';
import type { LiveSessionTransport } from './liveSessionTransport';
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
});
