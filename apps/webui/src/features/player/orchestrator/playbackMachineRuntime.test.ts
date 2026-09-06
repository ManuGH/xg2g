// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// @vitest-environment node

import { describe, expect, it, vi } from 'vitest';
import { createInitialPlaybackDomainState } from './playbackMachine';
import {
  createPlaybackMachineRuntime,
  PlaybackMachineRuntimeInstance,
} from './playbackMachineRuntime';
import type { PlaybackCommand } from './playbackTypes';

describe('PlaybackMachineRuntime (node environment)', () => {
  it('instantiates with initial state and provides stable state reference before changes', () => {
    const runtime = createPlaybackMachineRuntime(() => createInitialPlaybackDomainState(null));
    const s1 = runtime.getState();
    const s2 = runtime.getState();

    expect(s1.sessionPhase).toBe('idle');
    expect(s1).toBe(s2);
  });

  it('notifies listeners ONLY when state reference actually changes', () => {
    const runtime = createPlaybackMachineRuntime(() => createInitialPlaybackDomainState(null));
    const listener = vi.fn();
    const unsubscribe = runtime.subscribe(listener);

    // 1. Dispatch event that changes state (idle -> starting)
    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });

    expect(listener).toHaveBeenCalledTimes(1);
    expect(runtime.getState().sessionPhase).toBe('starting');

    // 2. Dispatch event that changes state (trace updated)
    runtime.dispatch({
      type: 'normative.playback.trace.updated',
      epoch: 1,
      traceId: 'trace-123',
    });

    expect(listener).toHaveBeenCalledTimes(2);
    expect(runtime.getState().traceId).toBe('trace-123');

    // 3. Dispatch stale event that DOES NOT change state
    runtime.dispatch({
      type: 'normative.playback.trace.updated',
      epoch: 0, // stale epoch
      traceId: 'stale-trace',
    });

    // Listener must NOT have been called for stale event
    expect(listener).toHaveBeenCalledTimes(2);
    expect(runtime.getState().traceId).toBe('trace-123');

    // 4. Unsubscribe works
    unsubscribe();
    runtime.dispatch({
      type: 'normative.playback.trace.updated',
      epoch: 1,
      traceId: 'trace-456',
    });

    expect(listener).toHaveBeenCalledTimes(2);
    expect(runtime.getState().traceId).toBe('trace-456');
  });

  it('executes commands even when state reference does not immediately change (e.g. stop intent)', () => {
    const executed: PlaybackCommand[] = [];
    const executor = vi.fn((cmd: PlaybackCommand) => {
      executed.push(cmd);
    });

    const runtime = createPlaybackMachineRuntime(
      () => createInitialPlaybackDomainState(null),
      executor,
    );
    const listener = vi.fn();
    runtime.subscribe(listener);

    // Start playback first
    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });

    expect(runtime.getState().sessionPhase).toBe('starting');
    expect(listener).toHaveBeenCalledTimes(1);
    executed.length = 0;
    listener.mockClear();

    // Send stop intent: state passes through (sessionPhase remains 'starting' until teardown completes),
    // but stop commands MUST be emitted immediately
    runtime.dispatch({
      type: 'intent.stop.requested',
      epoch: 2,
      reason: 'user_stop',
      notifyClose: true,
    });

    // Listener was NOT called because state didn't change yet
    expect(listener).not.toHaveBeenCalled();
    // But commands were executed
    expect(executor).toHaveBeenCalled();
    expect(executed).toEqual([
      { type: 'command.timeline.end_attempt', reason: 'user_stop' },
      { type: 'command.timeline.report', reason: 'user_stop' },
      { type: 'command.playback.stop', epoch: 2, reason: 'user_stop', notifyClose: true },
    ]);
  });

  it('supports full Start -> Stop -> Start lifecycle on the same runtime instance', () => {
    const executed: PlaybackCommand[] = [];
    const runtime = createPlaybackMachineRuntime(
      () => createInitialPlaybackDomainState(null),
      (cmd) => executed.push(cmd),
    );

    // Attempt 1: Start (sets playbackEpoch = 1, sessionEpoch = 0)
    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });
    expect(runtime.getState().sessionPhase).toBe('starting');

    // Attempt 1: Transition to ready
    runtime.dispatch({
      type: 'normative.session.phase.changed',
      playbackEpoch: 1,
      sessionEpoch: 0,
      phase: 'ready',
    });
    expect(runtime.getState().sessionPhase).toBe('ready');

    // Attempt 1: Stop intent
    runtime.dispatch({
      type: 'intent.stop.requested',
      epoch: 2,
      reason: 'user_stop',
      notifyClose: false,
    });

    // Attempt 1: Async teardown finishes -> normative stopped arrives
    runtime.dispatch({
      type: 'normative.playback.stopped',
      epoch: 2,
    });
    expect(runtime.getState().sessionPhase).toBe('stopped');

    // Attempt 2: Start again on same instance!
    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch: 3,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });
    expect(runtime.getState().sessionPhase).toBe('starting');
    expect(runtime.getState().epoch.playback).toBe(3);

    // Attempt 2: Becomes ready
    runtime.dispatch({
      type: 'normative.session.phase.changed',
      playbackEpoch: 3,
      sessionEpoch: 0,
      phase: 'ready',
    });
    expect(runtime.getState().sessionPhase).toBe('ready');
  });

  it('documents exact synchronous execution order for nested (re-entrant) dispatches', () => {
    // Nested dispatch occurs when an executor synchronously dispatches another event
    const log: string[] = [];

    const runtime = new PlaybackMachineRuntimeInstance(
      () => createInitialPlaybackDomainState(null),
      (command) => {
        log.push(`executor:${command.type}`);
        if (command.type === 'command.timeline.record') {
          // Synchronously trigger another event during command execution
          log.push('nested:dispatch:trace:start');
          runtime.dispatch({
            type: 'normative.playback.trace.updated',
            epoch: 1,
            traceId: 'nested-trace',
          });
          log.push('nested:dispatch:trace:end');
        }
      },
    );

    runtime.subscribe(() => {
      log.push(`listener:phase=${runtime.getState().sessionPhase}:trace=${runtime.getState().traceId}`);
    });

    log.push('outer:dispatch:start');
    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });
    log.push('outer:dispatch:end');

    // Verification of synchronous execution order:
    // 1. Outer dispatch updates state to 'starting' and notifies listener
    // 2. Command 'command.timeline.record' is passed to executor
    // 3. Executor synchronously calls dispatch('normative.playback.trace.updated')
    // 4. Inner dispatch updates trace to 'nested-trace' and notifies listener
    // 5. Inner dispatch completes, outer executor resumes
    // 6. Outer dispatch completes
    expect(log).toEqual([
      'outer:dispatch:start',
      'listener:phase=starting:trace=-',
      'executor:command.timeline.record',
      'nested:dispatch:trace:start',
      'listener:phase=starting:trace=nested-trace',
      'nested:dispatch:trace:end',
      'outer:dispatch:end',
    ]);
  });

  it('allows dynamically updating and clearing the command executor', () => {
    const executed1: PlaybackCommand[] = [];
    const executed2: PlaybackCommand[] = [];

    const runtime = createPlaybackMachineRuntime(
      () => createInitialPlaybackDomainState(null),
      (cmd) => executed1.push(cmd),
    );

    // 1. Initial executor receives commands
    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });
    expect(executed1.length).toBe(1);
    expect(executed2.length).toBe(0);

    // 2. Update to new executor
    runtime.setCommandExecutor((cmd) => executed2.push(cmd));
    runtime.dispatch({
      type: 'intent.stop.requested',
      epoch: 2,
      reason: 'user_stop',
      notifyClose: false,
    });
    // Next commands should go to executed2 ONLY
    expect(executed1.length).toBe(1);
    expect(executed2.length).toBe(3);

    // 3. Clear executor (e.g. during unmount / effect cleanup)
    runtime.setCommandExecutor(null);
    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch: 3,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });
    // Neither executor receives command
    expect(executed1.length).toBe(1);
    expect(executed2.length).toBe(3);
  });

  it('safely ignores dispatches and command execution after destroy()', () => {
    const executor = vi.fn();
    const listener = vi.fn();

    const runtime = createPlaybackMachineRuntime(
      () => createInitialPlaybackDomainState(null),
      executor,
    );
    runtime.subscribe(listener);

    runtime.destroy();

    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });

    expect(executor).not.toHaveBeenCalled();
    expect(listener).not.toHaveBeenCalled();
    expect(runtime.getState().sessionPhase).toBe('idle');
  });

  it('avoids infinite loops when a listener unsubscribes and re-subscribes during notification', () => {
    const runtime = createPlaybackMachineRuntime(() => createInitialPlaybackDomainState(null));
    let callCount = 0;
    let unsubscribe: () => void;

    const listener = () => {
      callCount++;
      // Unsubscribe and immediately re-subscribe during the callback
      unsubscribe();
      unsubscribe = runtime.subscribe(listener);
    };

    unsubscribe = runtime.subscribe(listener);

    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });

    // The listener must be called exactly once for this single state change
    expect(callCount).toBe(1);
  });

  it('skips listeners that were unsubscribed during the same iteration', () => {
    const runtime = createPlaybackMachineRuntime(() => createInitialPlaybackDomainState(null));
    const calls: string[] = [];

    const unsubBRef = { current: () => {} };

    const listenerA = () => {
      calls.push('A');
      // A unregisters B before B is visited
      unsubBRef.current();
    };

    const listenerB = () => {
      calls.push('B');
    };

    runtime.subscribe(listenerA);
    unsubBRef.current = runtime.subscribe(listenerB);

    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });

    expect(calls).toEqual(['A']);
  });

  it('stops listener notification and command execution if destroy() is called during iteration', () => {
    const executed: PlaybackCommand[] = [];
    const runtime = createPlaybackMachineRuntime(
      () => createInitialPlaybackDomainState(null),
      (cmd) => executed.push(cmd),
    );

    const calls: string[] = [];

    runtime.subscribe(() => {
      calls.push('first');
      // First listener destroys the runtime mid-iteration
      runtime.destroy();
    });

    runtime.subscribe(() => {
      calls.push('second');
    });

    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch: 1,
      playbackMode: 'LIVE',
      status: 'starting',
      requestedDuration: null,
    });

    expect(calls).toEqual(['first']);
    // Commands must also not be executed because runtime was destroyed
    expect(executed).toEqual([]);
  });
});
