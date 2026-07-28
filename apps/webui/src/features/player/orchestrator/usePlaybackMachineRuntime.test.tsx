import { StrictMode, useEffect, useRef } from 'react';
import { render, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { createInitialPlaybackDomainState, runPlaybackMachine } from './playbackMachine';
import { usePlaybackMachineRuntime } from './usePlaybackMachineRuntime';
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
});
