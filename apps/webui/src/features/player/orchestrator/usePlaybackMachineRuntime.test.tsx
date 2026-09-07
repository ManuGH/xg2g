import { StrictMode, Suspense, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { act, render, waitFor } from '@testing-library/react';
import type { Dispatch } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { createInitialPlaybackDomainState, runPlaybackMachine } from './playbackMachine';
import { usePlaybackMachineRuntime, type PlaybackCommandExecutor } from './usePlaybackMachineRuntime';
import type { PlaybackCommand, PlaybackMachineEvent } from './playbackTypes';


describe('runPlaybackMachine command derivation', () => {
  it('emits a timeline command when the session phase transitions', () => {
    const initial = createInitialPlaybackDomainState(null);
    const started = runPlaybackMachine(initial, {
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });

    expect(started.state.sessionPhase).toBe('starting');
    expect(started.commands).toEqual([
      { type: 'command.timeline.record', kind: 'session_phase', detail: 'starting' },
    ]);
  });

  it('emits no commands for stale events', () => {
    const initial = createInitialPlaybackDomainState(null);
    const started = runPlaybackMachine(initial, {
      type: 'normative.playback.attempt.started',
      epoch: 5,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });

    const stale = runPlaybackMachine(started.state, {
      type: 'normative.session.phase.changed',
      playbackEpoch: 4,
      sessionEpoch: 1,
      phase: 'ready',
    });

    expect(stale.state).toBe(started.state);
    expect(stale.commands).toEqual([]);
  });

  it('answers a stop intent with the full stop chain in imperative order', () => {
    const initial = createInitialPlaybackDomainState(null);
    const started = runPlaybackMachine(initial, {
      type: 'normative.playback.attempt.started',
      epoch: 3,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });

    const stopped = runPlaybackMachine(started.state, {
      type: 'intent.stop.requested',
      epoch: 4,
      reason: 'user_stop',
      notifyClose: true,
    });

    // Pass-through on state: the stopped transition arrives later via
    // 'normative.playback.stopped' once the async teardown finished.
    expect(stopped.state).toBe(started.state);
    expect(stopped.commands).toEqual([
      { type: 'command.timeline.end_attempt', reason: 'user_stop' },
      { type: 'command.timeline.report', reason: 'user_stop' },
      { type: 'command.playback.stop', epoch: 4, reason: 'user_stop', notifyClose: true },
    ]);
  });

  it('drops a stale stop intent without commands', () => {
    const initial = createInitialPlaybackDomainState(null);
    const started = runPlaybackMachine(initial, {
      type: 'normative.playback.attempt.started',
      epoch: 7,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });

    const stale = runPlaybackMachine(started.state, {
      type: 'intent.stop.requested',
      epoch: 6,
      reason: 'user_stop',
      notifyClose: false,
    });

    expect(stale.state).toBe(started.state);
    expect(stale.commands).toEqual([]);
  });

  it('emits no commands when the phase does not change', () => {
    const initial = createInitialPlaybackDomainState(null);
    const result = runPlaybackMachine(initial, {
      type: 'normative.playback.trace.updated',
      epoch: 0,
      traceId: 'req-1',
    });

    expect(result.commands).toEqual([]);
  });
});

describe('usePlaybackMachineRuntime under StrictMode', () => {
  it('executes commands exactly once per dispatched event despite double rendering', async () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((command: PlaybackCommand) => {
      executed.push(command);
    });
    const startEvent: PlaybackMachineEvent = {
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    };

    function Harness() {
      const [state, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      const dispatchedRef = useRef(false);
      useEffect(() => {
        // Guarded like real call sites; StrictMode re-runs effects after
        // remount, but the runtime must still only execute per dispatch call.
        if (!dispatchedRef.current) {
          dispatchedRef.current = true;
          dispatch(startEvent);
        }
      }, [dispatch]);
      return <div data-testid="phase">{state.sessionPhase}</div>;
    }

    const { getByTestId } = render(
      <StrictMode>
        <Harness />
      </StrictMode>,
    );

    await waitFor(() => {
      expect(getByTestId('phase').textContent).toBe('starting');
    });

    // StrictMode double-renders the component and re-mounts effects, but the
    // machine runs inside dispatch — one dispatch call, one command execution.
    expect(executor).toHaveBeenCalledTimes(1);
    expect(executed).toEqual([
      { type: 'command.timeline.record', kind: 'session_phase', detail: 'starting' },
    ]);
  });

  it('keeps dispatch identity stable and state consistent across re-renders', async () => {
    const dispatches: unknown[] = [];

    function Harness() {
      const [state, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        () => {},
      );
      dispatches.push(dispatch);
      const dispatchedRef = useRef(false);
      useEffect(() => {
        if (!dispatchedRef.current) {
          dispatchedRef.current = true;
          dispatch({
            type: 'normative.playback.trace.updated',
            epoch: 0,
            traceId: 'req-42',
          });
        }
      }, [dispatch]);
      return <div data-testid="trace">{state.traceId}</div>;
    }

    const { getByTestId } = render(
      <StrictMode>
        <Harness />
      </StrictMode>,
    );

    await waitFor(() => {
      expect(getByTestId('trace').textContent).toBe('req-42');
    });
    expect(new Set(dispatches).size).toBe(1);
  });

  it('processes subsequent events and commands correctly after StrictMode cleanup/setup', async () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((command: PlaybackCommand) => {
      executed.push(command);
    });

    let exposedDispatch!: Dispatch<PlaybackMachineEvent>;

    function Harness() {
      const [state, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      exposedDispatch = dispatch;

      const mountedRef = useRef(false);
      useEffect(() => {
        if (!mountedRef.current) {
          mountedRef.current = true;
          dispatch({
            type: 'normative.playback.attempt.started',
            epoch: 1,
            playbackMode: 'LIVE',
            status: 'starting',
            requestedDuration: null,
          });
        }
      }, [dispatch]);

      return <div data-testid="phase">{state.sessionPhase}</div>;
    }

    const { getByTestId } = render(
      <StrictMode>
        <Harness />
      </StrictMode>,
    );

    await waitFor(() => {
      expect(getByTestId('phase').textContent).toBe('starting');
    });
    expect(executed.length).toBe(1);

    // StrictMode has completed its Setup -> Cleanup -> Setup cycle.
    // Now trigger a secondary event: stop intent.
    act(() => {
      exposedDispatch({
        type: 'intent.stop.requested',
        epoch: 2,
        reason: 'user_stop',
        notifyClose: false,
      });
    });

    // Verify commands from second event executed cleanly despite StrictMode cleanup!
    expect(executed.length).toBe(4);
    expect(executed.slice(1)).toEqual([
      { type: 'command.timeline.end_attempt', reason: 'user_stop' },
      { type: 'command.timeline.report', reason: 'user_stop' },
      { type: 'command.playback.stop', epoch: 2, reason: 'user_stop', notifyClose: false },
    ]);
  });

  it('executes ONLY the updated executor after a re-render with a new executor function', async () => {
    const executedA: PlaybackCommand[] = [];
    const executedB: PlaybackCommand[] = [];
    const executorA = vi.fn((cmd: PlaybackCommand) => {
      executedA.push(cmd);
    });
    const executorB = vi.fn((cmd: PlaybackCommand) => {
      executedB.push(cmd);
    });

    let exposedDispatch!: Dispatch<PlaybackMachineEvent>;
    let setUseB!: (useB: boolean) => void;

    function Harness() {
      const [useB, setB] = useState(false);
      setUseB = setB;
      const currentExecutor = useB ? executorB : executorA;

      const [state, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        currentExecutor,
      );
      exposedDispatch = dispatch;

      return <div data-testid="epoch">{state.epoch.playback}</div>;
    }

    render(
      <StrictMode>
        <Harness />
      </StrictMode>,
    );

    // 1. Dispatch with executorA
    act(() => {
      exposedDispatch({
        type: 'normative.playback.attempt.started',
        epoch: 1,
        playbackMode: 'LIVE',
        status: 'starting',
        requestedDuration: null,
      });
    });

    expect(executorA).toHaveBeenCalledTimes(1);
    expect(executorB).not.toHaveBeenCalled();

    // 2. Re-render Harness with executorB
    act(() => {
      setUseB(true);
    });

    // 3. Next dispatch must execute ONLY executorB
    act(() => {
      exposedDispatch({
        type: 'intent.stop.requested',
        epoch: 2,
        reason: 'user_stop',
        notifyClose: false,
      });
    });

    expect(executorA).toHaveBeenCalledTimes(1);
    expect(executorB).toHaveBeenCalledTimes(3);
  });

  it('supports Start -> Stop -> Start across multiple attempts within the same mounted player', async () => {
    const executed: PlaybackCommand[] = [];
    let exposedDispatch!: Dispatch<PlaybackMachineEvent>;

    function Harness() {
      const [state, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        (cmd) => executed.push(cmd),
      );
      exposedDispatch = dispatch;
      return (
        <div>
          <span data-testid="phase">{state.sessionPhase}</span>
          <span data-testid="epoch">{state.epoch.playback}</span>
        </div>
      );
    }

    const { getByTestId } = render(
      <StrictMode>
        <Harness />
      </StrictMode>,
    );

    // Attempt 1: Start
    act(() => {
      exposedDispatch({
        type: 'normative.playback.attempt.started',
        epoch: 1,
        playbackMode: 'LIVE',
        status: 'starting',
        requestedDuration: null,
      });
    });
    expect(getByTestId('phase').textContent).toBe('starting');
    expect(getByTestId('epoch').textContent).toBe('1');

    // Attempt 1: Stop intent
    act(() => {
      exposedDispatch({
        type: 'intent.stop.requested',
        epoch: 2,
        reason: 'user_stop',
        notifyClose: false,
      });
    });

    // Attempt 1: Asynchronous teardown completed -> stopped
    act(() => {
      exposedDispatch({
        type: 'normative.playback.stopped',
        epoch: 2,
      });
    });
    expect(getByTestId('phase').textContent).toBe('stopped');

    // Attempt 2: Start again in the same mounted player!
    act(() => {
      exposedDispatch({
        type: 'normative.playback.attempt.started',
        epoch: 3,
        playbackMode: 'LIVE',
        status: 'starting',
        requestedDuration: null,
      });
    });
    expect(getByTestId('phase').textContent).toBe('starting');
    expect(getByTestId('epoch').textContent).toBe('3');

    // Attempt 2: Session ready
    act(() => {
      exposedDispatch({
        type: 'normative.session.phase.changed',
        playbackEpoch: 3,
        sessionEpoch: 0,
        phase: 'ready',
      });
    });
    expect(getByTestId('phase').textContent).toBe('ready');
  });

  it('does not execute commands when dispatched after unmount in StrictMode (disconnected executor)', () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((cmd: PlaybackCommand) => {
      executed.push(cmd);
    });

    let exposedDispatch!: Dispatch<PlaybackMachineEvent>;

    function Harness() {
      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      exposedDispatch = dispatch;
      return <div>Player</div>;
    }

    const { unmount } = render(
      <StrictMode>
        <Harness />
      </StrictMode>,
    );

    // Unmount the component: effect cleanup sets executor to null
    unmount();

    // Synchronous dispatch after unmount
    exposedDispatch({
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });

    expect(executor).not.toHaveBeenCalled();
    expect(executed).toEqual([]);
  });

  it('does not execute commands when dispatched synchronously after unmount WITHOUT StrictMode', () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((cmd: PlaybackCommand) => {
      executed.push(cmd);
    });

    let exposedDispatch!: Dispatch<PlaybackMachineEvent>;

    function Harness() {
      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      exposedDispatch = dispatch;
      return <div>Player</div>;
    }

    // Production-like mount: NO StrictMode wrapper (matches main.tsx:62)
    const { unmount } = render(<Harness />);

    // Immediate unmount: Cleanup 1 is the real unmount and must disconnect synchronously
    unmount();

    // Synchronous dispatch immediately following unmount
    exposedDispatch({
      type: 'intent.start.requested',
      epoch: 1,
      kind: 'live',
      serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
    });

    expect(executor).not.toHaveBeenCalled();
    expect(executed).toEqual([]);
  });

  it('does not execute commands when dispatched from pre-queued Promise after unmount WITHOUT StrictMode', async () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((cmd: PlaybackCommand) => {
      executed.push(cmd);
    });

    let exposedDispatch!: Dispatch<PlaybackMachineEvent>;

    function Harness() {
      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      exposedDispatch = dispatch;
      return <div>Player</div>;
    }

    const { unmount } = render(<Harness />);

    // Queue a microtask / promise callback while mounted
    const pendingPromise = Promise.resolve().then(() => {
      exposedDispatch({
        type: 'intent.start.requested',
        epoch: 1,
        kind: 'live',
        serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
      });
    });

    // Unmount before microtask runs
    unmount();

    // Wait for microtask callback to execute
    await pendingPromise;

    // The late dispatch must NOT execute commands through the disconnected executor
    expect(executor).not.toHaveBeenCalled();
    expect(executed).toEqual([]);
  });

  it('does not execute commands when dispatched from pre-queued Promise after unmount WITH StrictMode', async () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((cmd: PlaybackCommand) => {
      executed.push(cmd);
    });

    let exposedDispatch!: Dispatch<PlaybackMachineEvent>;

    function Harness() {
      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      exposedDispatch = dispatch;
      return <div>Player</div>;
    }

    const { unmount } = render(
      <StrictMode>
        <Harness />
      </StrictMode>,
    );

    const pendingPromise = Promise.resolve().then(() => {
      exposedDispatch({
        type: 'intent.start.requested',
        epoch: 1,
        kind: 'live',
        serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
      });
    });

    unmount();
    await pendingPromise;

    expect(executor).not.toHaveBeenCalled();
    expect(executed).toEqual([]);
  });

  it('executes commands for early dispatches in child component useEffect across both StrictMode setups', async () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((command: PlaybackCommand) => {
      executed.push(command);
    });

    let epochCounter = 1;

    function Child({ dispatch }: { dispatch: Dispatch<PlaybackMachineEvent> }) {
      useEffect(() => {
        dispatch({
          type: 'intent.start.requested',
          epoch: epochCounter++,
          kind: 'live',
          serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
        });
      }, [dispatch]);
      return null;
    }

    function Parent() {
      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      return <Child dispatch={dispatch} />;
    }

    render(
      <StrictMode>
        <Parent />
      </StrictMode>,
    );

    // In StrictMode, the effect runs twice (Setup 1 -> Cleanup 1 -> Setup 2).
    // Both setups MUST execute their commands (none dropped due to disconnected executor).
    expect(executor).toHaveBeenCalledTimes(2);
    expect(executed.length).toBe(2);
    expect(executed[0]).toMatchObject({ type: 'command.playback.start', epoch: 1 });
    expect(executed[1]).toMatchObject({ type: 'command.playback.start', epoch: 2 });
  });

  it('executes commands for early dispatches in useLayoutEffect across both StrictMode setups', async () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((command: PlaybackCommand) => {
      executed.push(command);
    });

    let epochCounter = 1;

    function Harness() {
      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      useLayoutEffect(() => {
        dispatch({
          type: 'intent.start.requested',
          epoch: epochCounter++,
          kind: 'live',
          serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
        });
      }, [dispatch]);
      return <div>Harness</div>;
    }

    render(
      <StrictMode>
        <Harness />
      </StrictMode>,
    );

    // In StrictMode, the layout effect runs twice (Setup 1 -> Cleanup 1 -> Setup 2).
    // Both setups MUST execute their commands.
    expect(executor).toHaveBeenCalledTimes(2);
    expect(executed.length).toBe(2);
    expect(executed[0]).toMatchObject({ type: 'command.playback.start', epoch: 1 });
    expect(executed[1]).toMatchObject({ type: 'command.playback.start', epoch: 2 });
  });

  it('executes early commands in child useEffect and useLayoutEffect WITHOUT StrictMode exactly once', () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((command: PlaybackCommand) => {
      executed.push(command);
    });

    function Child({ dispatch }: { dispatch: Dispatch<PlaybackMachineEvent> }) {
      useEffect(() => {
        dispatch({
          type: 'intent.start.requested',
          epoch: 1,
          kind: 'live',
          serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
        });
      }, [dispatch]);
      return null;
    }

    function Parent() {
      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      return <Child dispatch={dispatch} />;
    }

    render(<Parent />);

    expect(executor).toHaveBeenCalledTimes(1);
    expect(executed.length).toBe(1);
    expect(executed[0]).toMatchObject({ type: 'command.playback.start', epoch: 1 });
  });

  it('does not execute commands after unmount when StrictMode is applied only to a subtree (Codex Counterexample 1)', () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((cmd: PlaybackCommand) => {
      executed.push(cmd);
    });

    let exposedDispatch!: Dispatch<PlaybackMachineEvent>;

    function Harness() {
      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      exposedDispatch = dispatch;
      return <div>SubtreePlayer</div>;
    }

    // StrictMode is applied inside a non-StrictMode container (subtree StrictMode)
    const { unmount } = render(
      <div>
        <StrictMode>
          <Harness />
        </StrictMode>
      </div>,
    );

    unmount();

    exposedDispatch({
      type: 'intent.start.requested',
      epoch: 1,
      kind: 'live',
      serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
    });

    expect(executor).not.toHaveBeenCalled();
    expect(executed).toEqual([]);
  });

  it('does not execute commands after unmount when render-phase state update re-rendered before first commit (Codex Counterexample 2)', () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((cmd: PlaybackCommand) => {
      executed.push(cmd);
    });

    let exposedDispatch!: Dispatch<PlaybackMachineEvent>;

    function Harness() {
      // Conditional state update during render phase: triggers immediate re-render before commit
      const [reRendered, setReRendered] = useState(false);
      if (!reRendered) {
        setReRendered(true);
      }

      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      exposedDispatch = dispatch;
      return <div>ReRenderedPlayer</div>;
    }

    // Without StrictMode
    const { unmount } = render(<Harness />);

    unmount();

    exposedDispatch({
      type: 'intent.start.requested',
      epoch: 1,
      kind: 'live',
      serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
    });

    expect(executor).not.toHaveBeenCalled();
    expect(executed).toEqual([]);
  });

  it('executes commands for early dispatches in child component useLayoutEffect across both StrictMode setups', () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((command: PlaybackCommand) => {
      executed.push(command);
    });
    const calledImmediately: boolean[] = [];

    let epochCounter = 1;

    function Child({ dispatch }: { dispatch: Dispatch<PlaybackMachineEvent> }) {
      useLayoutEffect(() => {
        const countBefore = executor.mock.calls.length;
        dispatch({
          type: 'intent.start.requested',
          epoch: epochCounter++,
          kind: 'live',
          serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
        });
        const countAfter = executor.mock.calls.length;
        calledImmediately.push(countAfter === countBefore + 1);
      }, [dispatch]);
      return null;
    }

    function Parent() {
      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      return <Child dispatch={dispatch} />;
    }

    render(
      <StrictMode>
        <Parent />
      </StrictMode>,
    );

    // In StrictMode, child useLayoutEffect runs in Setup 1 and Setup 2.
    // Both setups must execute their commands synchronously BEFORE returning from dispatch().
    expect(calledImmediately).toEqual([true, true]);
    expect(executor).toHaveBeenCalledTimes(2);
    expect(executed.length).toBe(2);
    expect(executed[0]).toMatchObject({ type: 'command.playback.start', epoch: 1 });
    expect(executed[1]).toMatchObject({ type: 'command.playback.start', epoch: 2 });
  });

  it('executes commands in child useLayoutEffect upon executor change WITHOUT StrictMode', () => {
    const executedA: PlaybackCommand[] = [];
    const executedB: PlaybackCommand[] = [];
    const executorA = vi.fn((cmd: PlaybackCommand) => executedA.push(cmd));
    const executorB = vi.fn((cmd: PlaybackCommand) => executedB.push(cmd));

    let setExecutor!: (fn: PlaybackCommandExecutor) => void;
    let setEpoch!: (epoch: number) => void;

    function Child({
      dispatch,
      epoch,
    }: {
      dispatch: Dispatch<PlaybackMachineEvent>;
      epoch: number;
    }) {
      useLayoutEffect(() => {
        dispatch({
          type: 'intent.start.requested',
          epoch,
          kind: 'live',
          serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
        });
      }, [dispatch, epoch]);
      return null;
    }

    function Parent() {
      const [currentExec, setExec] = useState<PlaybackCommandExecutor>(() => executorA);
      const [currentEpoch, setCurrentEpoch] = useState(1);
      setExecutor = setExec;
      setEpoch = setCurrentEpoch;
      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        currentExec,
      );
      return <Child dispatch={dispatch} epoch={currentEpoch} />;
    }

    render(<Parent />);
    expect(executorA).toHaveBeenCalledTimes(1);
    expect(executorB).not.toHaveBeenCalled();

    // Re-render Parent with new executorB and new epoch
    act(() => {
      setExecutor(() => executorB);
      setEpoch(2);
    });

    // Parent re-renders, executor updated without temporary disconnection,
    // and subsequent dispatch executes exclusively on executorB
    expect(executorA).toHaveBeenCalledTimes(1);
    expect(executorB).toHaveBeenCalledTimes(1);
    expect(executedB[0]).toMatchObject({ type: 'command.playback.start', epoch: 2 });
  });

  it('executes commands in child useLayoutEffect upon executor change WITH StrictMode', () => {
    const executedA: PlaybackCommand[] = [];
    const executedB: PlaybackCommand[] = [];
    const executorA = vi.fn((cmd: PlaybackCommand) => executedA.push(cmd));
    const executorB = vi.fn((cmd: PlaybackCommand) => executedB.push(cmd));

    let setExecutor!: (fn: PlaybackCommandExecutor) => void;
    let setEpoch!: (epoch: number) => void;

    function Child({
      dispatch,
      epoch,
    }: {
      dispatch: Dispatch<PlaybackMachineEvent>;
      epoch: number;
    }) {
      useLayoutEffect(() => {
        dispatch({
          type: 'intent.start.requested',
          epoch,
          kind: 'live',
          serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
        });
      }, [dispatch, epoch]);
      return null;
    }

    function Parent() {
      const [currentExec, setExec] = useState<PlaybackCommandExecutor>(() => executorA);
      const [currentEpoch, setCurrentEpoch] = useState(1);
      setExecutor = setExec;
      setEpoch = setCurrentEpoch;
      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        currentExec,
      );
      return <Child dispatch={dispatch} epoch={currentEpoch} />;
    }

    render(
      <StrictMode>
        <Parent />
      </StrictMode>,
    );
    expect(executorA).toHaveBeenCalledTimes(2);
    expect(executorB).not.toHaveBeenCalled();

    // Re-render Parent with new executorB and new epoch
    act(() => {
      setExecutor(() => executorB);
      setEpoch(2);
    });

    expect(executorA).toHaveBeenCalledTimes(2);
    expect(executorB).toHaveBeenCalledTimes(1);
    expect(executedB[0]).toMatchObject({ type: 'command.playback.start', epoch: 2 });
  });

  it('executes commands after Suspense hide and reveal', async () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((cmd: PlaybackCommand) => executed.push(cmd));

    let exposedDispatch!: Dispatch<PlaybackMachineEvent>;
    let resolveSuspense!: () => void;
    let suspendPromise: Promise<void> | null = null;
    let setShouldSuspend!: (suspend: boolean) => void;

    function SuspendingSibling({ suspend }: { suspend: boolean }) {
      if (suspend) {
        if (!suspendPromise) {
          suspendPromise = new Promise<void>((resolve) => {
            resolveSuspense = resolve;
          });
        }
        throw suspendPromise;
      }
      return <div>Normal Content</div>;
    }

    function PlayerChild() {
      const [, dispatch] = usePlaybackMachineRuntime(
        () => createInitialPlaybackDomainState(null),
        executor,
      );
      exposedDispatch = dispatch;
      return <div>Player Active</div>;
    }

    function App() {
      const [suspend, setSuspend] = useState(false);
      setShouldSuspend = setSuspend;
      return (
        <Suspense fallback={<div>Loading Fallback</div>}>
          <PlayerChild />
          <SuspendingSibling suspend={suspend} />
        </Suspense>
      );
    }

    const { getByText } = render(<App />);
    expect(getByText('Player Active')).toBeTruthy();

    // Suspend the boundary: hides PlayerChild
    act(() => {
      setShouldSuspend(true);
    });
    expect(getByText('Loading Fallback')).toBeTruthy();

    // Reveal the boundary: reveals PlayerChild
    await act(async () => {
      setShouldSuspend(false);
      resolveSuspense();
      await suspendPromise;
    });
    expect(getByText('Player Active')).toBeTruthy();

    // Dispatch after reveal: executor must execute the command cleanly
    act(() => {
      exposedDispatch({
        type: 'intent.start.requested',
        epoch: 10,
        kind: 'live',
        serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
      });
    });

    expect(executor).toHaveBeenCalledTimes(1);
    expect(executed[0]).toMatchObject({ type: 'command.playback.start', epoch: 10 });
  });
});
