// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import {
  useInsertionEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react';
import type { Dispatch } from 'react';
import {
  createPlaybackMachineRuntime,
  type PlaybackCommandExecutor,
  type PlaybackMachineRuntime,
} from './playbackMachineRuntime';
import type {
  PlaybackDomainState,
  PlaybackMachineEvent,
} from './playbackTypes';

export type { PlaybackCommandExecutor, PlaybackMachineRuntime };

// Replaces useReducer(playbackMachine, ...) with a render-independent event
// loop backed by PlaybackMachineRuntime. The machine runs inside dispatch —
// a plain callback, never the render phase — so React StrictMode's double-invocation
// of render/reducers cannot re-run it: each dispatched event computes the transition
// once and executes its commands exactly once, synchronously.
//
// Ownership and lifecycle architecture:
// - Runtime instance is created once via useState and exposed via useSyncExternalStore.
// - Executor identity is decoupled from runtime lifecycle: executorRef is updated
//   synchronously on every render, so changing callbacks never causes effect tear-down.
// - Connection state and synchronous execution:
//   * Commands run immediately and synchronously before dispatch() returns.
//     No queues or buffers are used, preserving direct synchronous command execution (e.g. for stopStream).
//   * useInsertionEffect manages the true component mount/unmount lifecycle:
//     - React StrictMode's mount simulation (Setup 1 -> Cleanup 1 -> Setup 2) only re-runs
//       layout and passive effects; it does not clean up insertion effects. Thus the executor
//       remains active through Setup 2, allowing child useLayoutEffect to dispatch synchronously.
//     - On real unmount (non-StrictMode, subtree StrictMode, or post-simulation), the insertion effect
//       cleanup runs synchronously, disconnecting the executor so post-unmount dispatches drop commands.
//     - Maintenance note: React docs note useInsertionEffect is primarily intended for CSS-in-JS.
//       This lifecycle usage should be audited when upgrading major React versions.
//   * useLayoutEffect setup re-connects the executor on mount and when revealed from Suspense.
export function usePlaybackMachineRuntime(
  createInitialState: () => PlaybackDomainState,
  executeCommand: PlaybackCommandExecutor,
): [PlaybackDomainState, Dispatch<PlaybackMachineEvent>] {
  const executorRef = useRef<PlaybackCommandExecutor | null>(executeCommand);
  executorRef.current = executeCommand;

  const isConnectedRef = useRef(true);

  const [runtime] = useState(() =>
    createPlaybackMachineRuntime(createInitialState, (command) => {
      if (isConnectedRef.current && executorRef.current) {
        executorRef.current(command);
      }
    }),
  );

  useInsertionEffect(() => {
    isConnectedRef.current = true;
    runtime.setCommandExecutor((command) => {
      if (isConnectedRef.current && executorRef.current) {
        executorRef.current(command);
      }
    });

    return () => {
      isConnectedRef.current = false;
      runtime.setCommandExecutor(null);
    };
  }, [runtime]);

  useLayoutEffect(() => {
    isConnectedRef.current = true;
    runtime.setCommandExecutor((command) => {
      if (isConnectedRef.current && executorRef.current) {
        executorRef.current(command);
      }
    });
  }, [runtime]);

  const state = useSyncExternalStore(
    runtime.subscribe,
    runtime.getState,
    runtime.getState,
  );

  return [state, runtime.dispatch];
}
