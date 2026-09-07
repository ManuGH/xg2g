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
    fetchStreamInfo: vi.fn().mockResolvedValue({ status: 200, data: {}, headers: new Headers() }),
    postStartIntent: vi.fn().mockResolvedValue({ status: 200, data: { sessionId: 's1' }, headers: new Headers() }),
    waitForReady: vi.fn().mockResolvedValue({ sessionId: 's1', playbackUrl: 'http://test' }),
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

    // In StrictMode, child's useLayoutEffect runs and dispatches synchronously.
    expect(executed.length).toBeGreaterThanOrEqual(1);
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

  it('preserves controller across Suspense hiding and revealing', async () => {
    const transport = createDummyTransport();
    let controllerRef: any = null;
    let setSuspendedFn: ((s: boolean) => void) | null = null;

    let resolveSuspendingPromise: () => void;
    const suspendingPromise = new Promise<void>((r) => {
      resolveSuspendingPromise = r;
    });

    function MaybeSuspendingComponent({ suspended }: { suspended: boolean }) {
      if (suspended) {
        throw suspendingPromise;
      }
      return <div>Content</div>;
    }

    function App() {
      const [suspended, setSuspended] = useState(false);
      setSuspendedFn = setSuspended;

      const { controller } = usePlaybackController(
        transport,
        createInitialState,
        () => {},
      );
      controllerRef = controller;

      return (
        <Suspense fallback={<div>Loading...</div>}>
          <MaybeSuspendingComponent suspended={suspended} />
        </Suspense>
      );
    }

    render(<App />);

    const disposeSpy = vi.spyOn(controllerRef, 'dispose');

    expect(screen.getByText('Content')).toBeInTheDocument();

    // Suspend
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
  });
});
